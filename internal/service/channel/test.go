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

func (s *Service) TestModel(ctx context.Context, input adminapi.ModelTestInput) (TestResult, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, input.ModelID).Scan(&model); err != nil {
		return TestResult{}, gerror.Wrap(err, "find model")
	}
	if model.Id == 0 {
		return TestResult{}, gerror.New("model not found")
	}
	channel, err := s.Get(ctx, model.ChannelId)
	if err != nil {
		return TestResult{}, err
	}
	_, typeConfig, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return TestResult{}, err
	}
	endpoints := testEndpoints(input.Endpoint)
	var result TestResult
	for index, endpoint := range endpoints {
		current, path, tokens, requestErr := s.testModelEndpoint(ctx, channel, typeConfig, model, endpoint, input.Stream)
		if requestErr != nil {
			return TestResult{}, requestErr
		}
		result = current
		s.recordTestUsage(ctx, channel, model, path, &result, tokens)
		if result.Success || index == len(endpoints)-1 || !canTryAlternativeEndpoint(result) {
			break
		}
	}
	if result.Success {
		_ = s.app.Redis.Del(ctx, failureKey(channel.Id), cooldownKey(channel.Id)).Err()
		_, _ = s.resilience.RecoverIfAllowed(ctx, channel.Id)
	} else {
		_, _ = s.resilience.DisableIfNeeded(ctx, system.AutoDisableInput{ChannelID: channel.Id, Status: result.HTTPStatus, Latency: time.Duration(result.LatencyMs) * time.Millisecond, Message: result.Message})
	}
	s.saveTestResult(ctx, channel.Id, model.Id, result.Endpoint, result)
	return result, nil
}

func (s *Service) testModelEndpoint(ctx context.Context, channel entity.Channels, typeConfig channeltype.Config, model entity.ChannelModels, endpoint string, stream bool) (TestResult, string, usage.TokenUsage, error) {
	path, payload, streamed := testPayload(endpoint, model.UpstreamName, stream)
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.BaseUrl+path, bytes.NewReader(body))
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, gerror.Wrap(err, "create model test request")
	}
	req.Header.Set("Content-Type", "application/json")
	if err = s.setConfiguredHeaders(ctx, req, channel, typeConfig.Models.AuthType, typeConfig.Models.HeaderName, typeConfig.Models.HeaderPrefix); err != nil {
		return TestResult{}, path, usage.TokenUsage{}, err
	}
	startedAt := time.Now()
	resp, requestErr := s.app.HTTP.Do(req)
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

func (s *Service) recordTestUsage(ctx context.Context, channel entity.Channels, model entity.ChannelModels, path string, result *TestResult, tokens usage.TokenUsage) {
	if s.usage == nil {
		return
	}
	err := s.usage.Record(ctx, usage.RecordInput{
		RequestID:      usage.NewRequestID("aftest"),
		UserID:         usage.SystemUserID,
		ChannelID:      channel.Id,
		Endpoint:       "test:" + path,
		RequestedModel: model.PublicName,
		UpstreamModel:  model.UpstreamName,
		HTTPStatus:     result.HTTPStatus,
		Stream:         result.Stream,
		Tokens:         tokens,
		EstimatedCost:  usage.EstimatePublicModelCost(ctx, model.PublicName, path, tokens),
		DurationMs:     result.LatencyMs,
		Attempts:       1,
		ErrorMessage:   result.Message,
	})
	if err != nil {
		result.Message = truncate(result.Message+"；用量记录失败："+err.Error(), 1024)
	}
}

func testEndpoints(endpoint string) []string {
	if endpoint == "auto" {
		return []string{"chat", "responses", "embeddings"}
	}
	return []string{endpoint}
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

func (s *Service) StartHealthChecks(ctx context.Context) {
	go func() {
		var lastCheck time.Time
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				settings, err := s.resilience.Get(ctx)
				if err != nil || !settings.HealthCheckEnabled || !settings.RecoveryEnabled {
					continue
				}
				interval := time.Duration(settings.HealthCheckIntervalMinutes) * time.Minute
				if !lastCheck.IsZero() && now.Sub(lastCheck) < interval {
					continue
				}
				lastCheck = now
				s.runHealthChecks(ctx, settings.HealthCheckMode)
			}
		}
	}()
}

func (s *Service) runHealthChecks(ctx context.Context, mode string) {
	type healthCheckModel struct {
		ChannelID uint64 `orm:"channel_id"`
		ModelID   uint64 `orm:"model_id"`
	}
	rows := make([]healthCheckModel, 0)
	model := dao.ChannelModels.Ctx(ctx).As("m").
		Fields("m.channel_id,m.id AS model_id").
		InnerJoin(dao.Channels.Table()+" c", "c.id=m.channel_id").
		Where("m.enabled", 1).
		OrderAsc("m.channel_id").
		OrderAsc("m.id")
	if mode == "all" {
		model = model.Where("c.status=1 OR (c.status=0 AND c.auto_disabled_at IS NOT NULL)")
	} else {
		model = model.Where("c.status=0 AND c.auto_disabled_at IS NOT NULL")
	}
	if err := model.Scan(&rows); err != nil {
		return
	}
	seen := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.ChannelID]; exists {
			continue
		}
		seen[row.ChannelID] = struct{}{}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, _ = s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: row.ModelID, Endpoint: "chat"})
		cancel()
	}
}
