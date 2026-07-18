package relay

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/yunloli/aiferry/internal/service/channel"
	"github.com/yunloli/aiferry/internal/service/usage"
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

func TestRetryableStatus(t *testing.T) {
	for _, status := range []int{401, 403, 404, 408, 429, 500, 503} {
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
	body, err := prepareRequestBody("/chat/completions", []byte(`{"model":"public-model","messages":[{"role":"user","content":"你好"}],"service_tier":"flex","store":true,"include":["usage"],"unknown":"blocked"}`), "upstream-model", config)
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
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("default system prompt was not added: %#v", payload["messages"])
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
