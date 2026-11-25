package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/geminisub"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) testGeminiSubscriptionModel(ctx context.Context, ch entity.Channels, credential RouteCredential, model entity.ChannelModels, stream bool) (TestResult, string, usage.TokenUsage, error) {
	const path = "/chat/completions" // Bill and record the public protocol, not the vendor's RPC path.
	_, payload, _ := testPayload("chat", model.UpstreamName, false, channeltype.Config{}, adminapi.SystemResilienceSettingsInput{})
	body, err := json.Marshal(payload)
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, gerror.Wrap(err, "encode Gemini subscription model test")
	}
	body, err = geminisub.BuildRequest(body, model.UpstreamName, strings.TrimSpace(ch.ProjectId))
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, err
	}
	client, token, err := s.geminiSubscriptionClientToken(ctx, ch, credential)
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, err
	}
	req, err := newGeminiSubscriptionRequest(ctx, GeminiSubscriptionBaseURL+GeminiSubscriptionStreamPath, "text/event-stream", token, body)
	if err != nil {
		return TestResult{}, path, usage.TokenUsage{}, gerror.Wrap(err, "create Gemini subscription model test")
	}
	result, tokens := runGeminiSubscriptionModelTest(client, req, model.PublicName, stream)
	return result, path, tokens, nil
}

func runGeminiSubscriptionModelTest(client *http.Client, req *http.Request, model string, stream bool) (TestResult, usage.TokenUsage) {
	startedAt := time.Now()
	resp, err := client.Do(req)
	result := TestResult{Endpoint: "chat", Stream: stream, Model: model, LatencyMs: time.Since(startedAt).Milliseconds()}
	if err != nil {
		result.Message = "Gemini subscription model request failed"
		return result, usage.TokenUsage{}
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		result.Message = upstreamModelQueryError(resp.StatusCode, body).Error()
		return result, usage.TokenUsage{}
	}
	reader := &io.LimitedReader{R: resp.Body, N: (4 << 20) + 1}
	body, _, err := geminisub.ConvertResponse(reader, model, "chatcmpl-model-test")
	if reader.N == 0 {
		result.Message = "Gemini subscription model response exceeded the size limit"
		return result, usage.TokenUsage{}
	}
	if err != nil {
		result.Message = err.Error()
		return result, usage.TokenUsage{}
	}
	tokens := usage.ParseJSONUsage(body)
	result.Success = true
	result.Message = "模型响应正常"
	result.InputTokens = int64(testTokenValue(tokens.Input))
	result.OutputTokens = int64(testTokenValue(tokens.Output))
	return result, tokens
}
