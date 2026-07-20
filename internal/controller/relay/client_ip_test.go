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
