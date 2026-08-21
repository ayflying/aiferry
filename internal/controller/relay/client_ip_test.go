package relay

import (
	"net/http"
	"testing"
)

func TestClientIPFromHeadersUsesFirstValidForwardedAddress(t *testing.T) {
	headers := http.Header{"X-Forwarded-For": {"unknown, 203.0.113.9, 10.0.0.1"}}
	if actual := clientIPFromHeaders(headers, "198.51.100.1"); actual != "203.0.113.9" {
		t.Fatalf("unexpected client IP: %s", actual)
	}
}

func TestClientIPFromHeadersFallsBackToConnectionIP(t *testing.T) {
	if actual := clientIPFromHeaders(http.Header{}, "2001:db8::8"); actual != "2001:db8::8" {
		t.Fatalf("unexpected fallback IP: %s", actual)
	}
}

func TestClientIPFromHeadersSkipsInvalidProxyFallback(t *testing.T) {
	if actual := clientIPFromHeaders(http.Header{}, "unknown", "203.0.113.8:443"); actual != "203.0.113.8" {
		t.Fatalf("unexpected connection fallback IP: %s", actual)
	}
}

func TestNormalizedVideoClientResponseAddsBodyForEmptyFailure(t *testing.T) {
	headers := http.Header{"Trace-Id": []string{"upstream-trace-123"}}
	body, resultHeaders := normalizedVideoClientResponse(http.StatusBadRequest, nil, headers)
	if resultHeaders.Get("Trace-Id") != "upstream-trace-123" {
		t.Fatalf("Trace-Id = %q", resultHeaders.Get("Trace-Id"))
	}
	if resultHeaders.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", resultHeaders.Get("Content-Type"))
	}
	if resultHeaders.Get("X-AiFerry-Upstream-Status") != "400" {
		t.Fatalf("X-AiFerry-Upstream-Status = %q", resultHeaders.Get("X-AiFerry-Upstream-Status"))
	}
	want := `{"error":{"message":"Upstream video provider returned HTTP 400 without an error response body","type":"upstream_error"}}`
	if string(body) != want {
		t.Fatalf("body = %s", body)
	}
}

func TestNormalizedVideoClientResponsePreservesNonEmptyBody(t *testing.T) {
	original := []byte(`{"error":{"message":"provider rejected ratio"}}`)
	body, resultHeaders := normalizedVideoClientResponse(http.StatusBadRequest, original, http.Header{})
	if string(body) != string(original) {
		t.Fatalf("body = %s", body)
	}
	if resultHeaders.Get("X-AiFerry-Upstream-Status") != "" {
		t.Fatalf("unexpected upstream status header: %q", resultHeaders.Get("X-AiFerry-Upstream-Status"))
	}
}
