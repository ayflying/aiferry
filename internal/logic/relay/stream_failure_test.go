package relay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

func TestStreamTerminalLinesChatEmitsErrorThenDone(t *testing.T) {
	lines := streamTerminalLines(protocol.ChatCompletionsEndpoint, streamTerminal{
		status:  http.StatusPaymentRequired,
		message: "upstream balance is empty",
	})
	if len(lines) != 2 {
		t.Fatalf("expected an error frame and a done frame, got %d", len(lines))
	}
	payload := strings.TrimPrefix(strings.TrimSpace(string(lines[0])), "data: ")
	if !json.Valid([]byte(payload)) {
		t.Fatalf("error frame is not valid JSON: %q", payload)
	}
	if code := gjson.Get(payload, "error.code").Int(); code != int64(http.StatusPaymentRequired) {
		t.Fatalf("error.code = %d", code)
	}
	if message := gjson.Get(payload, "error.message").String(); message != "upstream balance is empty" {
		t.Fatalf("error.message = %q", message)
	}
	if !strings.Contains(string(lines[1]), "[DONE]") {
		t.Fatalf("stream must be closed with [DONE], got %q", lines[1])
	}
}

func TestStreamTerminalLinesResponsesEmitsErrorEvent(t *testing.T) {
	lines := streamTerminalLines(protocol.ResponsesEndpoint, streamTerminal{status: http.StatusBadGateway})
	if len(lines) != 1 {
		t.Fatalf("expected a single error event, got %d", len(lines))
	}
	frame := string(lines[0])
	if !strings.HasPrefix(frame, "event: error\n") {
		t.Fatalf("Responses clients read the event name, got %q", frame)
	}
	payload := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(frame, "event: error")), "data: ")
	if !json.Valid([]byte(payload)) {
		t.Fatalf("error event is not valid JSON: %q", payload)
	}
	if kind := gjson.Get(payload, "type").String(); kind != "error" {
		t.Fatalf("type = %q", kind)
	}
	if message := gjson.Get(payload, "error.message").String(); message != streamTruncatedReason {
		t.Fatalf("error.message = %q", message)
	}
}

func TestStreamTerminalLinesSanitizesStatusAndMessage(t *testing.T) {
	lines := streamTerminalLines(protocol.ChatCompletionsEndpoint, streamTerminal{status: http.StatusOK})
	payload := strings.TrimPrefix(strings.TrimSpace(string(bytes.TrimSpace(lines[0]))), "data: ")
	if code := gjson.Get(payload, "error.code").Int(); code != int64(http.StatusBadGateway) {
		t.Fatalf("a 2xx status must not be reported to the client, got code %d", code)
	}
}

func TestStreamTruncatedRequiresStreamAndWrittenOutput(t *testing.T) {
	tests := []struct {
		name      string
		stream    bool
		result    attemptResult
		truncated bool
	}{
		{name: "non-stream 200", result: attemptResult{status: http.StatusOK}},
		{name: "stream without output", stream: true, result: attemptResult{status: http.StatusOK}},
		{name: "stream completed", stream: true, result: attemptResult{status: http.StatusOK, wroteBytes: true, streamCompleted: true}},
		{name: "stream stopped after output", stream: true, result: attemptResult{status: http.StatusOK, wroteBytes: true}, truncated: true},
		{name: "stream stopped after output with error", stream: true, result: attemptResult{status: http.StatusPaymentRequired, wroteBytes: true, errorMessage: "upstream balance is empty"}, truncated: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := streamTruncated(test.stream, test.result); got != test.truncated {
				t.Fatalf("streamTruncated = %t, expected %t", got, test.truncated)
			}
		})
	}
}

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
	if !streamPayloadHasVisibleOutput([]byte("data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"thinking\"}\n")) {
		t.Fatal("reasoning summary delta should commit the stream")
	}
	if !streamPayloadHasVisibleOutput([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"thinking\"}}\n")) {
		t.Fatal("thinking delta should commit the stream")
	}
}
