package channel

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/upstreamerror"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
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

func (s *sChannel) TestModel(ctx context.Context, input adminapi.ModelTestInput, userID uint64) (TestResult, error) {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, input.ModelID).Scan(&model); err != nil {
		return TestResult{}, gerror.Wrap(err, "find model")
	}
	if model.Id == 0 {
		return TestResult{}, gerror.New("model not found")
	}
	if userID != usage.SystemUserID && s.prices.IsPriced(model.PublicName) {
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
	advancedConfig, err := ParseAdvancedConfig([]byte(channel.AdvancedConfig))
	if err != nil {
		return TestResult{}, err
	}
	baseURLs := advancedConfig.UpstreamBaseURLs(channel.BaseUrl)
	var (
		result     TestResult
		billingErr error
		path       string
		tokens     usage.TokenUsage
	)
	tested := false
	finished := false
	for index, endpoint := range endpoints {
		for _, baseURL := range baseURLs {
			current, currentPath, currentTokens, requestErr := s.testModelEndpoint(ctx, channel, credential, typeConfig, model, baseURL, endpoint, input.Stream)
			if requestErr != nil {
				return TestResult{}, requestErr
			}
			tested = true
			result, path, tokens = current, currentPath, currentTokens
			if result.Success {
				finished = true
				break
			}
		}
		if finished || index == len(endpoints)-1 || !canTryAlternativeEndpoint(result) {
			break
		}
	}
	if tested {
		billingErr = s.recordTestUsage(ctx, userID, channel, credential.ID, model, path, &result, tokens)
	}
	if result.Success {
		s.clearCredentialTransient(ctx, credential.ID)
		s.resilience.ClearAutoDisableFailures(ctx, credential.ID)
		_, _ = s.resilience.RecoverCredentialIfAllowed(ctx, credential.ID)
		_, _ = s.resilience.RecoverIfAllowed(ctx, channel.Id)
		// 测试成功同时恢复该模型本身并加分，让被禁用的模型有机会重新可用。
		_, _ = s.resilience.RecoverModelIfAllowed(ctx, model.Id)
		s.bumpModelHealthScore(ctx, model.Id, system.ModelHealthTestSuccess)
	} else {
		_, _ = s.resilience.DisableIfNeeded(ctx, system.AutoDisableInput{
			ChannelID:           channel.Id,
			ChannelCredentialID: credential.ID,
			ChannelModelID:      model.Id,
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

// buildTestRequest 按 payload 类型构造测试请求：asrMultipartRequest 走 multipart 表单，其余走 JSON。
func buildTestRequest(ctx context.Context, url string, payload any) (*http.Request, error) {
	if asr, ok := payload.(asrMultipartRequest); ok {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		_ = writer.WriteField("model", asr.Model)
		part, err := writer.CreateFormFile("file", asr.Filename)
		if err == nil {
			_, err = part.Write(asr.Content)
		}
		if err == nil {
			err = writer.Close()
		}
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (s *sChannel) testModelEndpoint(ctx context.Context, channel entity.Channels, credential RouteCredential, typeConfig channeltype.Config, model entity.ChannelModels, baseURL, endpoint string, stream bool) (TestResult, string, usage.TokenUsage, error) {
	path, payload, streamed := testPayload(endpoint, model.UpstreamName, stream)
	req, err := buildTestRequest(ctx, baseURL+path, payload)
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, gerror.Wrap(err, "create model test request")
	}
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
		result.Message = upstreamerror.Message(responseBody, resp.Status)
	}
	return result, path, tokens, nil
}

func (s *sChannel) recordTestUsage(ctx context.Context, userID uint64, channel entity.Channels, credentialID uint64, model entity.ChannelModels, path string, result *TestResult, tokens usage.TokenUsage) error {
	if s.usage == nil {
		return nil
	}
	cost := s.prices.Estimate(model.PublicName, path, tokens)
	recordStatus := result.HTTPStatus
	recordMessage := result.Message
	var chargeErr error
	if result.Success {
		if cost != nil {
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
	case containsAny(modelName, "tts", "speech"):
		return []string{"tts"}
	case containsAny(modelName, "asr", "whisper", "transcribe", "stt"):
		return []string{"asr"}
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

func containsAny(model string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(model, keyword) {
			return true
		}
	}
	return false
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
	case "tts":
		return "/audio/speech", map[string]any{
			"model":           model,
			"input":           "模型测试",
			"voice":           "alloy",
			"response_format": "mp3",
		}, false
	case "asr":
		return "/audio/transcriptions", asrTestPayload(model), false
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

// asrMultipartRequest 描述 ASR 测试的 multipart 表单：最小合法 WAV + 模型名。
type asrMultipartRequest struct {
	Model    string
	Filename string
	Content  []byte
}

// asrTestPayload 构造最小 WAV（44 字节头 + 1 个静音采样），足以让上游校验通过并返回转写结果。
func asrTestPayload(model string) asrMultipartRequest {
	return asrMultipartRequest{
		Model:    model,
		Filename: "aiferry-test.wav",
		Content:  minimalWAV(),
	}
}

// minimalWAV 返回单声道 8kHz 16bit 的 1 采样静音 WAV。
func minimalWAV() []byte {
	const (
		channels   = 1
		sampleRate = 8000
		bitsPerSam = 16
	)
	data := []byte{0x00, 0x00}
	byteRate := sampleRate * channels * bitsPerSam / 8
	blockAlign := channels * bitsPerSam / 8
	buffer := bytes.NewBuffer(nil)
	buffer.WriteString("RIFF")
	binary.Write(buffer, binary.LittleEndian, uint32(36+len(data)))
	buffer.WriteString("WAVE")
	buffer.WriteString("fmt ")
	binary.Write(buffer, binary.LittleEndian, uint32(16))
	binary.Write(buffer, binary.LittleEndian, uint16(1))
	binary.Write(buffer, binary.LittleEndian, uint16(channels))
	binary.Write(buffer, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buffer, binary.LittleEndian, uint32(byteRate))
	binary.Write(buffer, binary.LittleEndian, uint16(blockAlign))
	binary.Write(buffer, binary.LittleEndian, uint16(bitsPerSam))
	buffer.WriteString("data")
	binary.Write(buffer, binary.LittleEndian, uint32(len(data)))
	buffer.Write(data)
	return buffer.Bytes()
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

func (s *sChannel) saveTestResult(ctx context.Context, channelID, modelID uint64, endpoint string, result TestResult) {
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
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, channelID).Data(do.Channels{
		LastTestStatus:    status,
		LastTestLatencyMs: result.LatencyMs,
		LastTestError:     message,
		LastTestAt:        time.Now(),
	}).Update(); err == nil {
		s.InvalidateListCache(ctx)
	}
}

func testTokenValue(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
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
