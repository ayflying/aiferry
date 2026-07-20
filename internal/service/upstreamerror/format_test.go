package upstreamerror

import "testing"

func TestMessagePreservesStructuredUpstreamError(t *testing.T) {
	body := []byte(`{"error":{"code":429,"reason":"DAILY_LIMIT_EXCEEDED","message":"daily usage limit exceeded"}}`)
	want := `error: code=429 reason="DAILY_LIMIT_EXCEEDED" message="daily usage limit exceeded"`
	if got := Message(body, "429 Too Many Requests"); got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
}

func TestMessageFallsBackForInvalidJSON(t *testing.T) {
	if got := Message([]byte("gateway unavailable"), "HTTP 503"); got != "HTTP 503" {
		t.Fatalf("Message() = %q", got)
	}
}
