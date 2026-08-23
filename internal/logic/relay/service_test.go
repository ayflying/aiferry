package relay

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

func TestParseJSONUsageVariants(t *testing.T) {
	tokens := parseJSONUsage([]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30},"total_tokens":120}}`))
	if tokens.Input == nil || *tokens.Input != 100 || tokens.Output == nil || *tokens.Output != 20 || tokens.CachedInput == nil || *tokens.CachedInput != 30 {
		t.Fatalf("unexpected chat usage: %+v", tokens)
	}
	tokens = parseJSONUsage([]byte(`{"usage":{"input_tokens":12,"output_tokens":8}}`))
	if tokens.Total == nil || *tokens.Total != 20 {
		t.Fatalf("total should be derived: %+v", tokens)
	}
	tokens = parseJSONUsage([]byte(`{"usage":{"input_tokens":20,"cache_creation_input_tokens":4,"input_tokens_details":{"image_tokens":3,"audio_tokens":2},"output_tokens":7,"output_tokens_details":{"audio_tokens":5}}}`))
	if tokens.CacheWrite == nil || *tokens.CacheWrite != 4 || tokens.ImageInput == nil || *tokens.ImageInput != 3 || tokens.AudioInput == nil || *tokens.AudioInput != 2 || tokens.AudioOutput == nil || *tokens.AudioOutput != 5 {
		t.Fatalf("special usage details were not parsed: %+v", tokens)
	}
	tokens = parseJSONUsage([]byte(`{"usage":{"input_tokens":20,"input_tokens_details":{"cached_tokens":12,"cache_write_tokens":8},"output_tokens":7}}`))
	if tokens.CachedInput == nil || *tokens.CachedInput != 12 || tokens.CacheWrite == nil || *tokens.CacheWrite != 8 {
		t.Fatalf("GPT-5.6 cache usage was not parsed: %+v", tokens)
	}
}

func TestPromptCachePolicyUsesSystemKeyUnlessPassthroughEnabled(t *testing.T) {
	candidate := Candidate{ChannelID: 9, ChannelCredentialID: 2, PublicName: "gpt-5.6-terra"}
	body := []byte(`{"model":"gpt-5.6-terra","prompt_cache_key":"client-key","prompt_cache_options":{"mode":"implicit"},"input":[{"content":[{"type":"input_text","text":"hello","prompt_cache_breakpoint":{"mode":"explicit"}}]}]}`)

	systemBody, err := applyPromptCachePolicy(body, candidate, 42, channel.DefaultAdvancedConfig())
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(systemBody, "prompt_cache_key").String(); actual != stablePromptCacheKey(42, candidate) {
		t.Fatalf("system prompt_cache_key = %q", actual)
	}
	for _, path := range []string{"prompt_cache_options", "input.0.content.0.prompt_cache_breakpoint"} {
		if gjson.GetBytes(systemBody, path).Exists() {
			t.Fatalf("system-managed cache must remove %s: %s", path, systemBody)
		}
	}

	config := channel.DefaultAdvancedConfig()
	config.PassthroughPromptCache = true
	passthroughBody, err := applyPromptCachePolicy(body, candidate, 42, config)
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(passthroughBody, "prompt_cache_key").String(); actual != "client-key" {
		t.Fatalf("passthrough prompt_cache_key = %q", actual)
	}
	if actual := gjson.GetBytes(passthroughBody, "input.0.content.0.prompt_cache_breakpoint.mode").String(); actual != "explicit" {
		t.Fatalf("passthrough breakpoint = %q", actual)
	}
}

func TestParseSSEUsage(t *testing.T) {
	var tokens = parseJSONUsage(nil)
	parseSSEUsage([]byte("data: {\"usage\":{\"input_tokens\":9,\"output_tokens\":3,\"total_tokens\":12}}\n"), &tokens)
	if tokens.Total == nil || *tokens.Total != 12 {
		t.Fatalf("unexpected SSE usage: %+v", tokens)
	}
}

func TestWeightedOrderKeepsPriorityGroups(t *testing.T) {
	input := []Candidate{{ChannelID: 1, Priority: 5, Weight: 1}, {ChannelID: 2, Priority: 10, Weight: 2}, {ChannelID: 3, Priority: 5, Weight: 3}, {ChannelID: 4, Priority: 10, Weight: 1}}
	ordered := weightedOrder(input)
	if len(ordered) != len(input) {
		t.Fatalf("candidate count changed: %d", len(ordered))
	}
	if ordered[0].Priority != 10 || ordered[1].Priority != 10 || ordered[2].Priority != 5 || ordered[3].Priority != 5 {
		t.Fatalf("priority order changed: %+v", ordered)
	}
}

func TestCandidateBaseURLsUsesPrimaryThenDistinctBackups(t *testing.T) {
	urls := candidateBaseURLs(Candidate{BaseURL: "https://primary.example.com/v1/", BackupBaseURLs: []string{"https://cdn-a.example.com/v1", "https://primary.example.com/v1", "https://cdn-a.example.com/v1", "https://cdn-b.example.com/v1/"}})
	want := []string{"https://primary.example.com/v1", "https://cdn-a.example.com/v1", "https://cdn-b.example.com/v1"}
	if len(urls) != len(want) {
		t.Fatalf("URLs = %#v, want %#v", urls, want)
	}
	for index := range want {
		if urls[index] != want[index] {
			t.Fatalf("URLs = %#v, want %#v", urls, want)
		}
	}
}

func TestRetryableStatus(t *testing.T) {
	for _, status := range []int{401, 402, 403, 404, 408, 429, 500, 503} {
		if !retryableStatus(status) {
			t.Fatalf("status %d should retry", status)
		}
	}
	for _, status := range []int{200, 400, 422} {
		if retryableStatus(status) {
			t.Fatalf("status %d should not retry", status)
		}
	}
}

func TestNonRetryableClientFailureStopsCredentialTraversal(t *testing.T) {
	settings := system.DefaultResilienceSettings()
	if !nonRetryableClientFailure(attemptResult{status: http.StatusBadRequest, body: []byte(`{"error":{"code":"invalid_type","message":"image_url must be a string"}}`)}, nil, settings) {
		t.Fatal("image URL validation failure must stop retries")
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity} {
		if !nonRetryableClientFailure(attemptResult{status: status}, nil, settings) {
			t.Fatalf("upstream client error status %d must stop retries", status)
		}
	}
	if nonRetryableClientFailure(attemptResult{status: http.StatusNotFound}, nil, settings) {
		t.Fatal("404 model-not-found must allow failover to another configured channel")
	}
	if nonRetryableClientFailure(attemptResult{status: http.StatusBadRequest, body: []byte(`{"error":{"message":"Responses API is not supported"}}`)}, nil, settings) {
		t.Fatal("unsupported endpoint must retain protocol fallback")
	}
	if nonRetryableClientFailure(attemptResult{status: http.StatusTooManyRequests}, nil, settings) {
		t.Fatal("configured retryable status must continue retries")
	}
}

func TestUncommittedFailuresStayInsideCandidateRetry(t *testing.T) {
	tests := []struct {
		name       string
		result     attemptResult
		attemptErr error
		complete   bool
	}{
		{name: "upstream 400", result: attemptResult{status: http.StatusBadRequest}},
		{name: "upstream 401", result: attemptResult{status: http.StatusUnauthorized}},
		{name: "transport error", attemptErr: gerror.New("call upstream")},
		{name: "success", result: attemptResult{status: http.StatusOK}, complete: true},
		{name: "stream already written", result: attemptResult{status: http.StatusBadGateway, wroteBytes: true}, complete: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			completed := attemptCompleted(test.result, test.attemptErr)
			if completed != test.complete {
				t.Fatalf("completed = %t, expected %t", completed, test.complete)
			}
		})
	}
}

func TestMissingBillableUsageErrorIsDistinct(t *testing.T) {
	err := gerror.Wrap(ErrUpstreamUsageNotBillable, "record relay usage")
	if !errors.Is(err, ErrUpstreamUsageNotBillable) {
		t.Fatal("missing billable usage must be identifiable for internal failover")
	}
	if errors.Is(gerror.New("账户余额不足"), ErrUpstreamUsageNotBillable) {
		t.Fatal("balance errors must not be retried as upstream failures")
	}
}

func TestUnpricedModelUsesFreeBillingWithoutBalanceCheck(t *testing.T) {
	relay := &sRelay{prices: pricingcache.New()}
	if relay.requiresBalanceCheck("unpriced-model") {
		t.Fatal("unpriced model must not require a user balance")
	}
	cost, chargeable := pricedUsageCost(false, nil)
	if cost == nil || !cost.IsZero() {
		t.Fatalf("unpriced model cost = %v, want 0", cost)
	}
	if chargeable {
		t.Fatal("unpriced model must not debit user or channel balance")
	}
	if relay.missingBillableUsage(Candidate{PublicName: "unpriced-model"}, "/chat/completions", attemptResult{status: http.StatusOK}) {
		t.Fatal("unpriced model must not fail over for missing billable usage")
	}
}

func TestPricedModelStillRequiresMatchedUsage(t *testing.T) {
	cost, chargeable := pricedUsageCost(true, nil)
	if cost != nil || chargeable {
		t.Fatalf("priced model without matched usage = cost %v, chargeable %t", cost, chargeable)
	}
}

func TestRetryableAvailabilityErrorsUseServerRetryResponse(t *testing.T) {
	for _, sentinel := range []error{ErrNoAvailableChannel, ErrEligibleChannelsExhausted} {
		err := gerror.Wrap(sentinel, "route unavailable")
		if !IsRetryableAvailabilityError(err) {
			t.Fatalf("error %v should be retryable", sentinel)
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("error %v must retain its sentinel", sentinel)
		}
	}
	if IsRetryableAvailabilityError(gerror.New("invalid request")) {
		t.Fatal("invalid request must not be treated as retryable")
	}
	response := RetryableAvailabilityClientError()
	if response.Status != http.StatusServiceUnavailable || response.Type != "server_error" || response.RetryAfter != "2" {
		t.Fatalf("unexpected retry response: %+v", response)
	}
}

func TestFailedAttemptResultKeepsUpstreamErrorReason(t *testing.T) {
	result := failedAttemptResult(attemptResult{
		status: http.StatusBadRequest,
		body:   openAIError("invalid_request_error", "图片格式不受支持"),
	}, "All eligible channels failed")
	if result.errorMessage != `error: message="图片格式不受支持" type="invalid_request_error"` {
		t.Fatalf("error reason = %q", result.errorMessage)
	}
	if result.status != http.StatusBadRequest {
		t.Fatalf("status = %d", result.status)
	}
}

func TestFailedAttemptResultTurnsTransportFailureIntoGatewayError(t *testing.T) {
	result := failedAttemptResult(attemptResult{}, "call upstream: context deadline exceeded")
	if result.status != http.StatusBadGateway {
		t.Fatalf("status = %d", result.status)
	}
	if result.errorMessage != "call upstream: context deadline exceeded" {
		t.Fatalf("error reason = %q", result.errorMessage)
	}
	if message := upstreamError(result.body, ""); !strings.Contains(message, result.errorMessage) {
		t.Fatalf("response body did not retain error reason: %s", result.body)
	}
}

func TestRuleCostHonorsEndpointAndCachedInput(t *testing.T) {
	input, cached, output := uint64(1_000_000), uint64(200_000), uint64(500_000)
	cost, ok := ruleCost(`{"endpoint":"/chat/completions","inputTokensAtLeast":500000}`, `{"inputPerMillion":2,"cachedInputPerMillion":0.5,"outputPerMillion":8,"request":0.01}`, "/chat/completions", usage.TokenUsage{Input: &input, CachedInput: &cached, Output: &output})
	if !ok || !cost.Equal(decimalRequire("5.71")) {
		t.Fatalf("unexpected rule cost: %v, matched=%t", cost, ok)
	}
	if _, ok = ruleCost(`{"endpoint":"/embeddings"}`, `{"inputPerMillion":2,"outputPerMillion":8}`, "/chat/completions", usage.TokenUsage{Input: &input, Output: &output}); ok {
		t.Fatal("endpoint-mismatched rule should not apply")
	}
}

func TestRuleCostSupportsRequestOnlyPricing(t *testing.T) {
	cost, ok := ruleCost(`{}`, `{"request":0.01}`, "/images/generations", usage.TokenUsage{})
	if !ok || !cost.Equal(decimalRequire("0.01")) {
		t.Fatalf("unexpected request-only cost: %v, matched=%t", cost, ok)
	}
}

func TestPrepareRequestBodyBlocksOptionalFieldsByDefault(t *testing.T) {
	config := channel.DefaultAdvancedConfig()
	config.SystemPrompt = "渠道规则"
	body, err := prepareRequestBody("/chat/completions", []byte(`{"model":"public-model","messages":[{"role":"user","content":"你好"}],"prompt_cache_key":"support:stable","prompt_cache_options":{"mode":"implicit"},"service_tier":"flex","store":true,"include":["usage"],"unknown":"blocked"}`), "upstream-model", config)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "upstream-model" {
		t.Fatalf("model was not mapped: %#v", payload)
	}
	for _, field := range []string{"service_tier", "store", "include", "unknown"} {
		if _, ok := payload[field]; ok {
			t.Fatalf("%s should be blocked: %#v", field, payload)
		}
	}
	if payload["prompt_cache_key"] != "support:stable" || payload["prompt_cache_options"].(map[string]any)["mode"] != "implicit" {
		t.Fatalf("prompt cache fields should be forwarded: %#v", payload)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("default system prompt was not added: %#v", payload["messages"])
	}
}

func TestPrepareImageGenerationRequestPreservesOpenAIFields(t *testing.T) {
	config := channel.DefaultAdvancedConfig()
	body, err := prepareRequestBody("/images/generations", []byte(`{"model":"public-image","prompt":"A blue ferry","n":1,"size":"1024x1024","quality":"high","output_format":"png","unknown":"blocked"}`), "upstream-image", config)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "upstream-image" {
		t.Fatalf("model was not mapped: %#v", payload)
	}
	for _, field := range []string{"prompt", "n", "size", "quality", "output_format"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("%s should be forwarded: %#v", field, payload)
		}
	}
	if _, ok := payload["unknown"]; ok {
		t.Fatalf("unknown field should be blocked: %#v", payload)
	}
}

func TestPrepareRequestBodyAllowsConfiguredFieldsAndSystemPromptAppend(t *testing.T) {
	config := channel.DefaultAdvancedConfig()
	config.PassthroughRequestBody = true
	config.AllowServiceTier = true
	config.BlockStore = false
	config.AllowSafetyIdentifier = true
	config.AllowInclude = true
	config.AllowInferenceGeo = true
	config.SystemPrompt = "渠道规则"
	config.AppendSystemPrompt = true
	body, err := prepareRequestBody("/chat/completions", []byte(`{"model":"public-model","messages":[{"role":"system","content":"用户规则"}],"service_tier":"flex","store":true,"safety_identifier":"user-1","include":["usage"],"inference_geo":"cn","custom":{"enabled":true}}`), "upstream-model", config)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"service_tier", "store", "safety_identifier", "include", "inference_geo", "custom"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("%s should be forwarded: %#v", field, payload)
		}
	}
	messages := payload["messages"].([]any)
	message := messages[0].(map[string]any)
	if message["content"] != "渠道规则\n\n用户规则" {
		t.Fatalf("system prompt was not appended: %#v", message)
	}
}

func TestNormalizeResponseMovesReasoningIntoThinkContent(t *testing.T) {
	config := channel.DefaultAdvancedConfig()
	config.ReasoningToContent = true
	config.ForceOpenAIFormat = true
	body := normalizeResponseBody("/chat/completions", []byte(`{"choices":[{"message":{"reasoning_content":"先思考","content":"回答"}}]}`), "upstream-model", config)
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	choice := payload["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if message["content"] != "<think>先思考</think>回答" {
		t.Fatalf("unexpected normalized content: %#v", message)
	}
	if _, ok := message["reasoning_content"]; ok || payload["object"] != "chat.completion" {
		t.Fatalf("response was not normalized: %#v", payload)
	}
}

func TestNormalizeResponseKeepsMultimodalContent(t *testing.T) {
	config := channel.DefaultAdvancedConfig()
	config.ReasoningToContent = true
	body := normalizeResponseBody("/chat/completions", []byte(`{"choices":[{"message":{"reasoning_content":"先思考","content":[{"type":"text","text":"回答"}]}}]}`), "upstream-model", config)
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	choice := payload["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if _, ok := message["reasoning_content"]; !ok {
		t.Fatalf("reasoning content should stay with multimodal content: %#v", message)
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("multimodal content should not be replaced: %#v", message)
	}
}

func decimalRequire(value string) decimal.Decimal {
	result, err := decimal.NewFromString(value)
	if err != nil {
		panic(err)
	}
	return result
}
