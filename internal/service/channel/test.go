package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/usage"
)

type TestResult struct {
	Success      bool   `json:"success"`
	Endpoint     string `json:"endpoint"`
	Stream       bool   `json:"stream"`
	Model        string `json:"model"`
	LatencyMs    int64  `json:"latencyMs"`
	HTTPStatus   int    `json:"httpStatus"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	Message      string `json:"message"`
}

func (s *Service) TestModel(ctx context.Context, input adminapi.ModelTestInput, userID uint64) (TestResult, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, input.ModelID).Scan(&model); err != nil {
		return TestResult{}, gerror.Wrap(err, "find model")
	}
	if model.Id == 0 {
		return TestResult{}, gerror.New("model not found")
	}
	if userID != usage.SystemUserID {
		if !s.prices.IsPriced(model.PublicName) {
			return TestResult{}, gerror.New("当前模型未配置可用价格，无法测试计费")
		}
		if err := s.users.CheckBalance(ctx, userID); err != nil {
			return TestResult{}, err
		}
	}
	channel, err := s.Get(ctx, model.ChannelId)
	if err != nil {
		return TestResult{}, err
	}
	credential, err := s.CredentialForTest(ctx, channel.Id, input.ChannelCredentialID)
	if err != nil {
		return TestResult{}, err
	}
	_, typeConfig, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return TestResult{}, err
	}
	endpoints := testEndpoints(input.Endpoint, model.UpstreamName)
	var (
		result     TestResult
		billingErr error
	)
	for index, endpoint := range endpoints {
		current, path, tokens, requestErr := s.testModelEndpoint(ctx, channel, credential, typeConfig, model, endpoint, input.Stream)
		if requestErr != nil {
			return TestResult{}, requestErr
		}
		result = current
		billingErr = s.recordTestUsage(ctx, userID, channel, credential.ID, model, path, &result, tokens)
		if result.Success || index == len(endpoints)-1 || !canTryAlternativeEndpoint(result) {
			break
		}
	}
	if result.Success {
		s.clearCredentialTransient(ctx, credential.ID)
		_, _ = s.resilience.RecoverCredentialIfAllowed(ctx, credential.ID)
		_, _ = s.resilience.RecoverIfAllowed(ctx, channel.Id)
	} else {
		_, _ = s.resilience.DisableIfNeeded(ctx, system.AutoDisableInput{
			ChannelID:           channel.Id,
			ChannelCredentialID: credential.ID,
			Source:              system.AutoDisableSourceModelTest,
			Status:              result.HTTPStatus,
			Latency:             time.Duration(result.LatencyMs) * time.Millisecond,
			Message:             result.Message,
		})
	}
	s.saveTestResult(ctx, channel.Id, model.Id, result.Endpoint, result)
	if billingErr != nil {
		return result, billingErr
	}
	return result, nil
}

func (s *Service) testModelEndpoint(ctx context.Context, channel entity.Channels, credential RouteCredential, typeConfig channeltype.Config, model entity.ChannelModels, endpoint string, stream bool) (TestResult, string, usage.TokenUsage, error) {
	path, payload, streamed := testPayload(endpoint, model.UpstreamName, stream)
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.BaseUrl+path, bytes.NewReader(body))
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, gerror.Wrap(err, "create model test request")
	}
	req.Header.Set("Content-Type", "application/json")
	if err = s.setConfiguredHeaders(ctx, req, channel, credential.APIKeyCipher, typeConfig.Models.AuthType, typeConfig.Models.HeaderName, typeConfig.Models.HeaderPrefix); err != nil {
		return TestResult{}, path, usage.TokenUsage{}, err
	}
	startedAt := time.Now()
	client, clientErr := s.HTTPClientForProxy(channel.ProxyUrlCipher)
	if clientErr != nil {
		return TestResult{}, path, usage.TokenUsage{}, clientErr
	}
	resp, requestErr := client.Do(req)
	latency := time.Since(startedAt).Milliseconds()
	result := TestResult{Endpoint: endpoint, Stream: streamed, Model: model.PublicName, LatencyMs: latency}
	if requestErr != nil {
		result.Message = requestErr.Error()
		return result, path, usage.TokenUsage{}, nil
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	result.HTTPStatus = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	tokens := parseTestUsage(responseBody, streamed)
	result.InputTokens = int64(testTokenValue(tokens.Input))
	result.OutputTokens = int64(testTokenValue(tokens.Output))
	if result.Success {
		result.Message = "模型响应正常"
	} else {
		result.Message = upstreamError(responseBody, resp.Status)
	}
	return result, path, tokens, nil
}

func (s *Service) recordTestUsage(ctx context.Context, userID uint64, channel entity.Channels, credentialID uint64, model entity.ChannelModels, path string, result *TestResult, tokens usage.TokenUsage) error {
	if s.usage == nil {
		return nil
	}
	cost := s.prices.Estimate(model.PublicName, path, tokens)
	recordStatus := result.HTTPStatus
	recordMessage := result.Message
	var chargeErr error
	if result.Success {
		if cost == nil && userID != usage.SystemUserID {
			chargeErr = gerror.New("上游响应未返回可计费的用量信息")
		} else if cost != nil {
			if applyErr := s.ApplyCredentialUsageCost(ctx, channel.Id, credentialID, *cost); applyErr != nil {
				g.Log().Warningf(ctx, "apply channel %d test usage cost: %v", channel.Id, applyErr)
			}
			if userID != usage.SystemUserID {
				if debitErr := s.users.Debit(ctx, userID, *cost); debitErr != nil {
					chargeErr = debitErr
				} else if s.mail != nil {
					s.mail.NotifyLowBalance(ctx, userID)
				}
			}
		}
		if chargeErr != nil {
			recordStatus = http.StatusPaymentRequired
			recordMessage = chargeErr.Error()
		}
	}
	recordErr := s.usage.Record(ctx, usage.RecordInput{
		RequestID:           usage.NewRequestID("aftest"),
		UserID:              userID,
		ChannelID:           channel.Id,
		ChannelCredentialID: credentialID,
		Endpoint:            "test:" + path,
		RequestedModel:      model.PublicName,
		UpstreamModel:       model.UpstreamName,
		HTTPStatus:          recordStatus,
		Stream:              result.Stream,
		Tokens:              tokens,
		EstimatedCost:       cost,
		DurationMs:          result.LatencyMs,
		Attempts:            1,
		ErrorMessage:        recordMessage,
	})
	if recordErr != nil {
		result.Message = truncate(result.Message+"；用量记录失败："+recordErr.Error(), 1024)
	}
	return chargeErr
}

func testEndpoints(endpoint, model string) []string {
	if endpoint != "auto" {
		return []string{endpoint}
	}
	modelName := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(modelName, "image"):
		return []string{"images"}
	case strings.Contains(modelName, "embedding"):
		return []string{"embeddings"}
	case strings.HasPrefix(modelName, "gpt-5"):
		return []string{"responses", "chat"}
	default:
		return []string{"chat", "responses", "embeddings"}
	}
}

func canTryAlternativeEndpoint(result TestResult) bool {
	switch result.HTTPStatus {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		message := strings.ToLower(result.Message)
		for _, marker := range []string{"endpoint", "not support", "unsupported", "not compatible", "only supports", "chat completion", "responses api", "embedding model"} {
			if strings.Contains(message, marker) {
				return true
			}
		}
	}
	return false
}

func testPayload(endpoint, model string, stream bool) (string, any, bool) {
	switch endpoint {
	case "responses":
		payload := map[string]any{
			"model":             model,
			"input":             "Reply with exactly OK.",
			"max_output_tokens": 16,
		}
		if stream {
			payload["stream"] = true
		}
		return "/responses", payload, stream
	case "embeddings":
		return "/embeddings", map[string]any{"model": model, "input": "AiFerry model check"}, false
	case "images":
		return "/images/generations", map[string]any{
			"model":  model,
			"prompt": "A small white ferry sailing on calm blue water.",
			"n":      1,
			"size":   "1024x1024",
		}, false
	default:
		payload := map[string]any{
			"model":                 model,
			"messages":              []map[string]string{{"role": "user", "content": "Reply with exactly OK."}},
			"max_completion_tokens": 16,
			"stream":                stream,
		}
		if stream {
			payload["stream_options"] = map[string]bool{"include_usage": true}
		}
		return "/chat/completions", payload, stream
	}
}

func parseTestUsage(body []byte, stream bool) usage.TokenUsage {
	tokens := usage.ParseJSONUsage(body)
	if !stream {
		return tokens
	}
	for _, line := range strings.Split(string(body), "\n") {
		usage.ParseSSEUsage([]byte(line), &tokens)
	}
	return tokens
}

func (s *Service) saveTestResult(ctx context.Context, channelID, modelID uint64, endpoint string, result TestResult) {
	status := "failed"
	if result.Success {
		status = "success"
	}
	message := truncate(result.Message, 1024)
	_, _ = dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, modelID).Data(do.ChannelModels{
		LastTestEndpoint:  endpoint,
		LastTestStatus:    status,
		LastTestLatencyMs: result.LatencyMs,
		LastTestError:     message,
		LastTestAt:        time.Now(),
	}).Update()
	_, _ = dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, channelID).Data(do.Channels{
		LastTestStatus:    status,
		LastTestLatencyMs: result.LatencyMs,
		LastTestError:     message,
		LastTestAt:        time.Now(),
	}).Update()
}

func testTokenValue(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func upstreamError(body []byte, fallback string) string {
	for _, path := range []string{"error.message", "message", "error"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return fallback
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func failureKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:failures", channelID)
}

func cooldownKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:cooldown", channelID)
}
