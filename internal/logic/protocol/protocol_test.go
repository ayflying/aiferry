package protocol

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestChatRequestToResponses(t *testing.T) {
	body, err := chatRequestToResponses([]byte(`{
  "model":"gpt-test","messages":[
    {"role":"system","content":"Follow policy"},
    {"role":"user","content":"Hello"}
  ],
  "max_completion_tokens":128,
  "tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
  "tool_choice":{"type":"function","function":{"name":"lookup"}}
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "instructions").String(); actual != "Follow policy" {
		t.Fatalf("instructions = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.0.role").String(); actual != "user" {
		t.Fatalf("input role = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.0.content").String(); actual != "Hello" {
		t.Fatalf("input content = %q", actual)
	}
	if actual := gjson.GetBytes(body, "max_output_tokens").Int(); actual != 128 {
		t.Fatalf("max_output_tokens = %d", actual)
	}
	if actual := gjson.GetBytes(body, "tools.0.name").String(); actual != "lookup" {
		t.Fatalf("tool name = %q", actual)
	}
	if actual := gjson.GetBytes(body, "tool_choice.name").String(); actual != "lookup" {
		t.Fatalf("tool choice = %q", actual)
	}
}

func TestChatToolContinuationToResponsesIncludesFunctionCallItems(t *testing.T) {
	body, err := chatRequestToResponses([]byte(`{
  "model":"gpt-test",
  "messages":[
    {"role":"assistant","content":[{"type":"text","text":"Let me check."}],"tool_calls":[{"id":"call_lookup","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
    {"role":"tool","tool_call_id":"call_lookup","content":"Sunny"}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "input.0.content.0.type").String(); actual != "output_text" {
		t.Fatalf("assistant content type = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.1.type").String(); actual != "function_call" {
		t.Fatalf("function call type = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.1.id").String(); actual != "call_lookup" {
		t.Fatalf("function call id = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.1.call_id").String(); actual != "call_lookup" {
		t.Fatalf("function call call_id = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.1.name").String(); actual != "lookup" {
		t.Fatalf("function call name = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.2.type").String(); actual != "function_call_output" {
		t.Fatalf("function output type = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.2.call_id").String(); actual != "call_lookup" {
		t.Fatalf("function output call_id = %q", actual)
	}
}

func TestChatAssistantContentToResponsesUsesSupportedTypes(t *testing.T) {
	body, err := chatRequestToResponses([]byte(`{
  "model":"gpt-test",
  "messages":[
    {"role":"assistant","content":"Answer"},
    {"role":"assistant","content":null,"refusal":"I cannot help with that."}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "input.0.content.0.type").String(); actual != "output_text" {
		t.Fatalf("assistant text type = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.1.content.0.type").String(); actual != "refusal" {
		t.Fatalf("assistant refusal type = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.1.content.0.refusal").String(); actual != "I cannot help with that." {
		t.Fatalf("assistant refusal = %q", actual)
	}
}

func TestChatImageContentToResponsesUsesStringURL(t *testing.T) {
	body, err := chatRequestToResponses([]byte(`{
  "model":"gpt-test",
  "messages":[{"role":"user","content":[
    {"type":"text","text":"Describe this image"},
    {"type":"image_url","image_url":{"url":"https://example.com/image.png","detail":"high"}},
    {"type":"image_url","image_url":"data:image/png;base64,AAAA"}
  ]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"input.0.content.1.image_url", "input.0.content.2.image_url"} {
		value := gjson.GetBytes(body, path)
		if value.Type != gjson.String {
			t.Fatalf("%s must be a string, got %s", path, value.Raw)
		}
	}
	if actual := gjson.GetBytes(body, "input.0.content.1.image_url").String(); actual != "https://example.com/image.png" {
		t.Fatalf("object image URL = %q", actual)
	}
}

func TestMultimodalProtocolConversion(t *testing.T) {
	responsesBody, err := chatRequestToResponses([]byte(`{
  "model":"gpt-test","messages":[{"role":"user","content":[
    {"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA","detail":"high"}},
    {"type":"file","file":{"file_id":"file_chat","filename":"notes.txt"}},
    {"type":"input_audio","input_audio":{"data":"QUJD","format":"wav"}}
  ]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(responsesBody, "input.0.content.0.image_url"); actual.Type != gjson.String || actual.String() != "data:image/png;base64,AAAA" {
		t.Fatalf("Responses image URL = %s", actual.Raw)
	}
	if actual := gjson.GetBytes(responsesBody, "input.0.content.0.detail").String(); actual != "high" {
		t.Fatalf("Responses image detail = %q", actual)
	}
	if actual := gjson.GetBytes(responsesBody, "input.0.content.1.file_id").String(); actual != "file_chat" {
		t.Fatalf("Responses file id = %q", actual)
	}
	if actual := gjson.GetBytes(responsesBody, "input.0.content.2.input_audio.format").String(); actual != "wav" {
		t.Fatalf("Responses audio format = %q", actual)
	}

	chatBody, err := responsesRequestToChat([]byte(`{
  "model":"gpt-test","input":[{"role":"user","content":[
    {"type":"input_image","image_url":"https://example.com/image.png","detail":"low"},
    {"type":"input_file","file_id":"file_response","filename":"report.pdf"},
    {"type":"input_audio","input_audio":{"data":"REVG","format":"mp3"}}
  ]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(chatBody, "messages.0.content.0.image_url.url").String(); actual != "https://example.com/image.png" {
		t.Fatalf("Chat image URL = %q", actual)
	}
	if actual := gjson.GetBytes(chatBody, "messages.0.content.0.image_url.detail").String(); actual != "low" {
		t.Fatalf("Chat image detail = %q", actual)
	}
	if actual := gjson.GetBytes(chatBody, "messages.0.content.1.file.file_id").String(); actual != "file_response" {
		t.Fatalf("Chat file id = %q", actual)
	}
	if actual := gjson.GetBytes(chatBody, "messages.0.content.2.input_audio.format").String(); actual != "mp3" {
		t.Fatalf("Chat audio format = %q", actual)
	}
}

func TestChatAssistantMultimodalContentToResponses(t *testing.T) {
	body, err := chatRequestToResponses([]byte(`{
  "model":"gpt-test","messages":[{"role":"assistant","content":[
    {"type":"text","text":"I saw this"},
    {"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"high"}},
    {"type":"file","file":{"file_id":"file_assistant","filename":"a.txt"}},
    {"type":"input_audio","input_audio":{"data":"QUJD","format":"wav"}}
  ]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ path, want string }{
		{"input.0.content.0.type", "output_text"},
		{"input.0.content.1.type", "input_image"},
		{"input.0.content.1.image_url", "https://example.com/a.png"},
		{"input.0.content.1.detail", "high"},
		{"input.0.content.2.type", "input_file"},
		{"input.0.content.2.file_id", "file_assistant"},
		{"input.0.content.3.type", "input_audio"},
	} {
		if actual := gjson.GetBytes(body, check.path).String(); actual != check.want {
			t.Fatalf("%s = %q, want %q", check.path, actual, check.want)
		}
	}
}

func TestNestedToolOutputImageToResponses(t *testing.T) {
	body, err := chatRequestToResponses([]byte(`{
  "model":"gpt-test","messages":[{"role":"tool","tool_call_id":"call_1","content":[{"type":"image_url","image_url":{"url":"https://example.com/nested.png","detail":"low"}}]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "input.0.output.0.type").String(); actual != "input_image" {
		t.Fatalf("nested output type = %q body=%s", actual, body)
	}
	if actual := gjson.GetBytes(body, "input.0.output.0.image_url").String(); actual != "https://example.com/nested.png" {
		t.Fatalf("nested output image URL = %q", actual)
	}
}

func TestInvalidFileDownloadDoesNotTriggerProtocolFallback(t *testing.T) {
	body := []byte(`{"error":{"code":"invalid_value","message":"Error while downloading file. Upstream status code: 404.","type":"invalid_request_error"}}`)
	if ShouldFallback(400, body) {
		t.Fatal("invalid file download must not trigger protocol fallback")
	}
}

func TestGPTChatPrefersResponsesAndKeepsCacheOptions(t *testing.T) {
	plan := PreferredPlan(ChatCompletionsEndpoint, "GPT-5.6-terra")
	if plan.upstreamEndpoint != ResponsesEndpoint || plan.conversion != chatToResponsesConversion {
		t.Fatalf("GPT Chat plan = %+v, want Chat to Responses conversion", plan)
	}
	if fallback, ok := AlternatePlan(ChatCompletionsEndpoint, plan); !ok || fallback.upstreamEndpoint != ChatCompletionsEndpoint || fallback.conversion != "" {
		t.Fatalf("GPT fallback plan = %+v, want direct Chat", fallback)
	}
	body, err := plan.ConvertRequest([]byte(`{
  "model":"gpt-5.6-terra",
  "prompt_cache_key":"support:stable",
  "prompt_cache_options":{"mode":"implicit"},
  "messages":[{"role":"user","content":[{"type":"text","text":"Hello","prompt_cache_breakpoint":{"mode":"explicit"}}]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "prompt_cache_key").String(); actual != "support:stable" {
		t.Fatalf("prompt_cache_key = %q", actual)
	}
	if actual := gjson.GetBytes(body, "prompt_cache_options.mode").String(); actual != "implicit" {
		t.Fatalf("prompt_cache_options.mode = %q", actual)
	}
	if actual := gjson.GetBytes(body, "input.0.content.0.prompt_cache_breakpoint.mode").String(); actual != "explicit" {
		t.Fatalf("prompt cache breakpoint = %q", actual)
	}
}

func TestNonGPTChatKeepsDirectProtocol(t *testing.T) {
	plan := PreferredPlan(ChatCompletionsEndpoint, "deepseek-v4-pro")
	if plan.upstreamEndpoint != ChatCompletionsEndpoint || plan.conversion != "" {
		t.Fatalf("non-GPT Chat plan = %+v, want direct Chat", plan)
	}
}

func TestNonGPTResponsesPrefersChat(t *testing.T) {
	plan := PreferredPlan(ResponsesEndpoint, "deepseek-v4-pro")
	if plan.upstreamEndpoint != ChatCompletionsEndpoint || plan.conversion != responsesToChatConversion {
		t.Fatalf("non-GPT Responses plan = %+v, want Responses to Chat conversion", plan)
	}
	if fallback, ok := AlternatePlan(ResponsesEndpoint, plan); !ok || fallback.upstreamEndpoint != ResponsesEndpoint || fallback.conversion != "" {
		t.Fatalf("non-GPT Responses fallback plan = %+v, want direct Responses", fallback)
	}

	body, err := plan.ConvertRequest([]byte(`{
  "model":"deepseek-v4-pro","stream":true,"instructions":"Follow policy",
  "input":[{"role":"user","content":[{"type":"input_text","text":"Hello"}]}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "messages.0.role").String(); actual != "system" {
		t.Fatalf("system role = %q", actual)
	}
	if actual := gjson.GetBytes(body, "messages.1.content.0.text").String(); actual != "Hello" {
		t.Fatalf("user content = %q", actual)
	}
}

func TestGPTResponsesKeepsDirectProtocol(t *testing.T) {
	plan := PreferredPlan(ResponsesEndpoint, "gpt-5.6-terra")
	if plan.upstreamEndpoint != ResponsesEndpoint || plan.conversion != "" {
		t.Fatalf("GPT Responses plan = %+v, want direct Responses", plan)
	}
	if fallback, ok := AlternatePlan(ResponsesEndpoint, plan); !ok || fallback.upstreamEndpoint != ChatCompletionsEndpoint || fallback.conversion != responsesToChatConversion {
		t.Fatalf("GPT Responses fallback plan = %+v, want Responses to Chat conversion", fallback)
	}
}

func TestResponsesRequestToChat(t *testing.T) {
	body, err := responsesRequestToChat([]byte(`{
  "model":"gpt-test","instructions":"Follow policy",
  "input":[{"role":"user","content":[{"type":"input_text","text":"Hello"}]}],
  "max_output_tokens":128,
  "tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
  "tool_choice":{"type":"function","name":"lookup"}
}`))
	if err != nil {
		t.Fatal(err)
	}
	if actual := gjson.GetBytes(body, "messages.0.role").String(); actual != "system" {
		t.Fatalf("system role = %q", actual)
	}
	if actual := gjson.GetBytes(body, "messages.1.content.0.text").String(); actual != "Hello" {
		t.Fatalf("user content = %q", actual)
	}
	if actual := gjson.GetBytes(body, "max_completion_tokens").Int(); actual != 128 {
		t.Fatalf("max_completion_tokens = %d", actual)
	}
	if actual := gjson.GetBytes(body, "tools.0.function.name").String(); actual != "lookup" {
		t.Fatalf("tool name = %q", actual)
	}
	if actual := gjson.GetBytes(body, "tool_choice.function.name").String(); actual != "lookup" {
		t.Fatalf("tool choice = %q", actual)
	}
}

func TestProtocolResponseConversionPreservesUsage(t *testing.T) {
	chatPlan, _ := fallbackPlan(ChatCompletionsEndpoint)
	chatBody := chatPlan.ConvertResponse([]byte(`{
  "id":"resp_1","object":"response","created_at":10,"model":"gpt-test","status":"completed",
  "output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}],
  "usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10}
}`))
	if actual := gjson.GetBytes(chatBody, "choices.0.message.content").String(); actual != "Hello" {
		t.Fatalf("chat content = %q", actual)
	}
	if actual := gjson.GetBytes(chatBody, "usage.prompt_tokens").Int(); actual != 7 {
		t.Fatalf("prompt tokens = %d", actual)
	}

	responsePlan, _ := fallbackPlan(ResponsesEndpoint)
	responseBody := responsePlan.ConvertResponse([]byte(`{
  "id":"chat_1","object":"chat.completion","created":10,"model":"gpt-test",
  "choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
}`))
	if actual := gjson.GetBytes(responseBody, "output.0.content.0.text").String(); actual != "Hello" {
		t.Fatalf("response content = %q", actual)
	}
	if actual := gjson.GetBytes(responseBody, "usage.input_tokens").Int(); actual != 7 {
		t.Fatalf("input tokens = %d", actual)
	}
}

func TestProtocolStreamConversion(t *testing.T) {
	chatPlan, _ := fallbackPlan(ChatCompletionsEndpoint)
	chatConverter := NewStreamConverter(chatPlan)
	chatConverter.Transform([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-test\",\"created_at\":10}}\n"))
	chatLines := chatConverter.Transform([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n"))
	if output := protocolStreamText(chatLines); !strings.Contains(output, "chat.completion.chunk") || !strings.Contains(output, "Hello") {
		t.Fatalf("unexpected Responses to Chat stream: %s", output)
	}
	completed := chatConverter.Transform([]byte("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":3,\"total_tokens\":10}}}\n"))
	if output := protocolStreamText(completed); !strings.Contains(output, "prompt_tokens") || !strings.Contains(output, "[DONE]") {
		t.Fatalf("unexpected Responses completion: %s", output)
	}

	responsePlan, _ := fallbackPlan(ResponsesEndpoint)
	responseConverter := NewStreamConverter(responsePlan)
	responseLines := responseConverter.Transform([]byte("data: {\"id\":\"chat_1\",\"created\":10,\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n"))
	if output := protocolStreamText(responseLines); !strings.Contains(output, "response.created") || !strings.Contains(output, "response.output_text.delta") {
		t.Fatalf("unexpected Chat to Responses stream: %s", output)
	}
	completed = responseConverter.Transform([]byte("data: [DONE]\n"))
	if output := protocolStreamText(completed); !strings.Contains(output, "response.completed") {
		t.Fatalf("unexpected Chat completion: %s", output)
	}
}

func TestProtocolFallbackOnlyForUnsupportedEndpoints(t *testing.T) {
	if !ShouldFallback(404, nil) {
		t.Fatal("404 should trigger protocol fallback")
	}
	if !ShouldFallback(400, []byte(`{"error":{"message":"Responses API is not supported"}}`)) {
		t.Fatal("unsupported endpoint message should trigger protocol fallback")
	}
	if ShouldFallback(400, []byte(`{"error":{"message":"temperature is invalid"}}`)) {
		t.Fatal("ordinary validation failure must not trigger protocol fallback")
	}
	if ShouldFallback(400, []byte(`{"error":{"code":"invalid_type","message":"image_url must be a string"}}`)) {
		t.Fatal("image URL validation failure must not trigger protocol fallback")
	}
}

func protocolStreamText(lines [][]byte) string {
	var result strings.Builder
	for _, line := range lines {
		result.Write(line)
	}
	return result.String()
}
