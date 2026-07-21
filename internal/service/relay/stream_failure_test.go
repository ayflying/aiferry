package relay

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseStreamFailurePreservesPaymentRequiredDetails(t *testing.T) {
	failure, ok := parseStreamFailure([]byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":402,\"type\":\"insufficient_quota\",\"message\":\"upstream balance is empty\"}}}\n"))
	if !ok {
		t.Fatal("expected stream failure")
	}
	if failure.status != http.StatusPaymentRequired {
		t.Fatalf("status = %d", failure.status)
	}
	for _, expected := range []string{"insufficient_quota", "upstream balance is empty"} {
		if !strings.Contains(failure.message, expected) || !strings.Contains(string(failure.body), expected) {
			t.Fatalf("missing %q in failure: %#v", expected, failure)
		}
	}
}

func TestStreamPayloadHasVisibleOutputSkipsPrelude(t *testing.T) {
	if streamPayloadHasVisibleOutput([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n")) {
		t.Fatal("response.created should remain retryable")
	}
	if !streamPayloadHasVisibleOutput([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n")) {
		t.Fatal("output delta should commit the stream")
	}
}
