package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFinishFailedRequestPreserves4xxAndPayloadPolicy(t *testing.T) {
	previous := payloadLogState
	defer func() { payloadLogState = previous }()
	for _, enabled := range []bool{true, false} {
		for _, stream := range []bool{false, true} {
			for _, status := range []int{400, 401, 403, 422, 429, 499} {
				t.Run(fmt.Sprintf("enabled=%t/stream=%t/status=%d", enabled, stream, status), func(t *testing.T) {
					var (
						dir       = t.TempDir()
						writer    = httptest.NewRecorder()
						request   = []byte(`{"model":"public","messages":[{"role":"user","content":"redacted"}]}`)
						response  = []byte("{\n  \"error\": {\"message\":\"original upstream failure\",\"code\":11128}\n}")
						candidate = Candidate{ChannelID: 7, ChannelName: "fixture", UpstreamName: "upstream"}
						result    = attemptResult{status: status, body: response, headers: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"fixture-id"}}, upstreamEndpoint: "/chat/completions"}
					)
					InitPayloadLog(enabled, dir, 3)
					(&sRelay{}).finishFailedRequest(writer, "afreq_failure", candidate, "public", "/chat/completions", stream, 2, request, result, nil)
					if writer.Code != status || !bytes.Equal(writer.Body.Bytes(), response) {
						t.Fatalf("response changed: status=%d body=%q", writer.Code, writer.Body.Bytes())
					}
					if writer.Header().Get("Content-Type") != "application/json" || writer.Header().Get("X-Request-Id") != "fixture-id" {
						t.Fatalf("headers changed: %v", writer.Header())
					}
					if !enabled {
						entries, err := os.ReadDir(dir)
						if err != nil || len(entries) != 0 {
							t.Fatalf("disabled payload logging wrote files: %v, %v", entries, err)
						}
						return
					}
					entry := waitFailedPayload(t, dir, "afreq_failure")
					if entry.HTTPStatus != status || entry.Attempts != 2 || entry.IsStream != stream || entry.ChannelID != 7 || entry.RequestedModel != "public" || entry.UpstreamModel != "upstream" || entry.Endpoint != "/chat/completions" || entry.UpstreamEndpoint != "/chat/completions" {
						t.Fatalf("incorrect payload metadata: %+v", entry)
					}
					if !jsonEqual(entry.Request, request) || !jsonEqual(entry.Response, response) {
						t.Fatalf("payload changed: request=%s response=%s", entry.Request, entry.Response)
					}
					entries, err := os.ReadDir(dir)
					if err != nil || len(entries) != 1 || entries[0].Name() != "afreq_failure.json" {
						t.Fatalf("expected exactly one payload file: %v, %v", entries, err)
					}
					if payloadLogState.maxFiles != 3 {
						t.Fatal("failure finalization changed retention configuration")
					}
				})
			}
		}
	}
}

func TestFinishFailedRequestKeepsRedactedLogAndRestoredClientResponse(t *testing.T) {
	previous := payloadLogState
	defer func() { payloadLogState = previous }()
	var (
		dir         = t.TempDir()
		writer      = httptest.NewRecorder()
		placeholder = "aiferry_ref_fixture_b_"
		restorer    = &sensitiveDataRestorer{replacements: map[string]string{placeholder: "private@example.invalid"}}
		body        = []byte(`{"model":"public","input":"` + placeholder + `"}`)
		response    = []byte(`{"error":{"message":"invalid ` + placeholder + `"}}`)
	)
	InitPayloadLog(true, dir, 3)
	(&sRelay{}).finishFailedRequest(writer, "afreq_redacted", Candidate{}, "public", "/responses", false, 1, body, attemptResult{status: 400, body: response}, restorer)
	if writer.Code != 400 || writer.Body.String() != `{"error":{"message":"invalid private@example.invalid"}}` {
		t.Fatalf("client restoration changed: status=%d body=%s", writer.Code, writer.Body.String())
	}
	entry := waitFailedPayload(t, dir, "afreq_redacted")
	if !jsonEqual(entry.Request, body) || !jsonEqual(entry.Response, response) || bytes.Contains(entry.Request, []byte("private@example.invalid")) || bytes.Contains(entry.Response, []byte("private@example.invalid")) {
		t.Fatalf("payload must keep existing redacted bodies: %+v", entry)
	}
}

func TestFinishFailedRequestDoesNotRewriteOtherFailures(t *testing.T) {
	previous := payloadLogState
	defer func() { payloadLogState = previous }()
	for _, result := range []attemptResult{
		{status: 502, body: []byte(`{"error":"gateway failure"}`)},
		{status: 400, wroteBytes: true, body: []byte(`{"error":"already written"}`)},
	} {
		dir := t.TempDir()
		InitPayloadLog(true, dir, 3)
		writer := httptest.NewRecorder()
		writer.WriteHeader(202)
		_, _ = writer.Write([]byte("existing response"))
		(&sRelay{}).finishFailedRequest(writer, "afreq_other", Candidate{}, "public", "/responses", true, 1, nil, result, nil)
		if writer.Code != 202 || writer.Body.String() != "existing response" {
			t.Fatalf("non-passthrough response changed: %d %s", writer.Code, writer.Body.String())
		}
		entry := waitFailedPayload(t, dir, "afreq_other")
		if entry.HTTPStatus != result.status || !jsonEqual(entry.Response, result.body) {
			t.Fatalf("other failure payload changed: %+v", entry)
		}
	}
}

func waitFailedPayload(t *testing.T, dir, requestID string) payloadEntry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(dir, requestID+".json"))
		if err == nil {
			var entry payloadEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				t.Fatal(err)
			}
			return entry
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			t.Fatalf("payload write did not complete: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func jsonEqual(left, right []byte) bool {
	var l, r bytes.Buffer
	return json.Compact(&l, left) == nil && json.Compact(&r, right) == nil && bytes.Equal(l.Bytes(), r.Bytes())
}
