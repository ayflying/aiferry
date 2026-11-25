package channel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRunGeminiSubscriptionModelTest(t *testing.T) {
	const good = `data: {"response":{"candidates":[{"content":{"parts":[{"text":"OK"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}}` + "\n\n"
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid SSE with usage", good, http.StatusOK, true},
		{"stream with no text", `data: {"response":{}}` + "\n\n", http.StatusOK, false},
		{"HTTP failure", `{"error":{"message":"private upstream details"}}`, http.StatusForbidden, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: geminiModelsRoundTripper(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, GeminiSubscriptionBaseURL+GeminiSubscriptionStreamPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, tokens := runGeminiSubscriptionModelTest(client, req, "public-model-name", false)
			if got.Success != tc.ok || got.HTTPStatus != tc.status || got.Model != "public-model-name" || got.Stream || got.Endpoint != "chat" || strings.Contains(got.Message, "private upstream details") {
				t.Fatalf("unexpected probe result: %+v", got)
			}
			if tc.ok && (got.InputTokens != 3 || got.OutputTokens != 2 || testTokenValue(tokens.Input) != 3 || testTokenValue(tokens.Output) != 2) {
				t.Fatalf("unexpected usage: %+v, %+v", got, tokens)
			}
		})
	}
}
