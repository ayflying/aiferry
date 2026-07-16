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
)

type TestResult struct {
	Success      bool   `json:"success"`
	Endpoint     string `json:"endpoint"`
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
	apiKey, err := s.app.Secrets.Decrypt(channel.ApiKeyCipher)
	if err != nil {
		return TestResult{}, err
	}
	path, payload := testPayload(input.Endpoint, model.UpstreamName)
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channel.BaseUrl+path, bytes.NewReader(body))
	if err != nil {
		return TestResult{}, gerror.Wrap(err, "create model test request")
	}
	req.Header.Set("Content-Type", "application/json")
	setUpstreamHeaders(req, channel, apiKey)
	startedAt := time.Now()
	resp, requestErr := s.app.HTTP.Do(req)
	latency := time.Since(startedAt).Milliseconds()
	result := TestResult{Endpoint: input.Endpoint, Model: model.PublicName, LatencyMs: latency}
	if requestErr != nil {
		result.Message = requestErr.Error()
		s.saveTestResult(ctx, channel.Id, model.Id, input.Endpoint, result)
		return result, nil
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	result.HTTPStatus = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	result.InputTokens = firstInt(responseBody, "usage.input_tokens", "usage.prompt_tokens")
	result.OutputTokens = firstInt(responseBody, "usage.output_tokens", "usage.completion_tokens")
	if result.Success {
		result.Message = "模型响应正常"
		_ = s.app.Redis.Del(ctx, failureKey(channel.Id), cooldownKey(channel.Id)).Err()
	} else {
		result.Message = upstreamError(responseBody, resp.Status)
	}
	s.saveTestResult(ctx, channel.Id, model.Id, input.Endpoint, result)
	return result, nil
}

func testPayload(endpoint, model string) (string, any) {
	switch endpoint {
	case "responses":
		return "/responses", map[string]any{
			"model":             model,
			"input":             "Reply with exactly OK.",
			"max_output_tokens": 16,
		}
	case "embeddings":
		return "/embeddings", map[string]any{"model": model, "input": "AiFerry model check"}
	default:
		return "/chat/completions", map[string]any{
			"model":                 model,
			"messages":              []map[string]string{{"role": "user", "content": "Reply with exactly OK."}},
			"max_completion_tokens": 16,
			"stream":                false,
		}
	}
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

func firstInt(body []byte, paths ...string) int64 {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Exists() {
			return value.Int()
		}
	}
	return 0
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
