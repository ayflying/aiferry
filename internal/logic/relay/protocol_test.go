package relay

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
	chatPlan, _ := fallbackProtocolPlan(chatCompletionsEndpoint)
	chatBody := chatPlan.convertResponse([]byte(`{
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

	responsePlan, _ := fallbackProtocolPlan(responsesEndpoint)
	responseBody := responsePlan.convertResponse([]byte(`{
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
	chatPlan, _ := fallbackProtocolPlan(chatCompletionsEndpoint)
	chatConverter := newProtocolStreamConverter(chatPlan)
	chatConverter.Transform([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-test\",\"created_at\":10}}\n"))
	chatLines := chatConverter.Transform([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n"))
	if output := protocolStreamText(chatLines); !strings.Contains(output, "chat.completion.chunk") || !strings.Contains(output, "Hello") {
		t.Fatalf("unexpected Responses to Chat stream: %s", output)
	}
	completed := chatConverter.Transform([]byte("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":3,\"total_tokens\":10}}}\n"))
	if output := protocolStreamText(completed); !strings.Contains(output, "prompt_tokens") || !strings.Contains(output, "[DONE]") {
		t.Fatalf("unexpected Responses completion: %s", output)
	}

	responsePlan, _ := fallbackProtocolPlan(responsesEndpoint)
	responseConverter := newProtocolStreamConverter(responsePlan)
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
	if !shouldFallbackWithProtocolConversion(404, nil) {
		t.Fatal("404 should trigger protocol fallback")
	}
	if !shouldFallbackWithProtocolConversion(400, []byte(`{"error":{"message":"Responses API is not supported"}}`)) {
		t.Fatal("unsupported endpoint message should trigger protocol fallback")
	}
	if shouldFallbackWithProtocolConversion(400, []byte(`{"error":{"message":"temperature is invalid"}}`)) {
		t.Fatal("ordinary validation failure must not trigger protocol fallback")
	}
}

func protocolStreamText(lines [][]byte) string {
	var result strings.Builder
	for _, line := range lines {
		result.Write(line)
	}
	return result.String()
}
