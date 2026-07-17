package channel

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTestEndpointsUsesAlternativesForAutoMode(t *testing.T) {
	endpoints := testEndpoints("auto")
	if len(endpoints) != 3 || endpoints[0] != "chat" || endpoints[1] != "responses" || endpoints[2] != "embeddings" {
		t.Fatalf("unexpected auto endpoints: %#v", endpoints)
	}
	if endpoints = testEndpoints("responses"); len(endpoints) != 1 || endpoints[0] != "responses" {
		t.Fatalf("explicit endpoint should not expand: %#v", endpoints)
	}
}

func TestTestPayloadAddsStreamUsageForChat(t *testing.T) {
	path, payload, streamed := testPayload("chat", "gpt-test", true)
	if path != "/chat/completions" || !streamed {
		t.Fatalf("unexpected chat payload metadata: path=%q streamed=%t", path, streamed)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if value["stream"] != true {
		t.Fatalf("chat stream flag was not enabled: %#v", value)
	}
	options, ok := value["stream_options"].(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Fatalf("chat stream usage was not requested: %#v", value)
	}
}

func TestTestPayloadKeepsEmbeddingsNonStreaming(t *testing.T) {
	path, _, streamed := testPayload("embeddings", "text-embedding-3-small", true)
	if path != "/embeddings" || streamed {
		t.Fatalf("embeddings should remain non-streaming: path=%q streamed=%t", path, streamed)
	}
}

func TestParseTestUsageReadsSSEUsage(t *testing.T) {
	tokens := parseTestUsage([]byte("data: {\"usage\":{\"input_tokens\":8,\"output_tokens\":3,\"total_tokens\":11}}\n\ndata: [DONE]\n"), true)
	if tokens.Input == nil || *tokens.Input != 8 || tokens.Output == nil || *tokens.Output != 3 || tokens.Total == nil || *tokens.Total != 11 {
		t.Fatalf("unexpected stream usage: %+v", tokens)
	}
	if !canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusNotFound}) ||
		!canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusBadRequest, Message: "This model does not support chat completions"}) ||
		canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusBadRequest, Message: "The requested model does not exist"}) ||
		canTryAlternativeEndpoint(TestResult{HTTPStatus: http.StatusTooManyRequests}) {
		t.Fatal("endpoint fallback statuses are incorrect")
	}
}
