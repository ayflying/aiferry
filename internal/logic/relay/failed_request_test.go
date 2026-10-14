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

// 最终 5xx 与已写出内容的失败同样保持既有语义：已写出的结果无法重放，就地收尾不重写；
// 最终 5xx（所有候选耗尽）此前只写状态不写体、客户端拿到空 200，现在同样透传上游原始
// 错误体——502 的上游 body 原样到达客户端，仍不做任何改写。
func TestFinishFailedRequestSkipsWrittenResults(t *testing.T) {
	previous := payloadLogState
	defer func() { payloadLogState = previous }()
	dir := t.TempDir()
	InitPayloadLog(true, dir, 3)
	writer := httptest.NewRecorder()
	writer.WriteHeader(202)
	_, _ = writer.Write([]byte("existing response"))
	result := attemptResult{status: 400, wroteBytes: true, body: []byte(`{"error":"already written"}`)}
	(&sRelay{}).finishFailedRequest(writer, "afreq_other", Candidate{}, "public", "/responses", true, 1, nil, result, nil)
	if writer.Code != 202 || writer.Body.String() != "existing response" {
		t.Fatalf("written response changed: %d %s", writer.Code, writer.Body.String())
	}
	entry := waitFailedPayload(t, dir, "afreq_other")
	if entry.HTTPStatus != result.status || !jsonEqual(entry.Response, result.body) {
		t.Fatalf("written failure payload changed: %+v", entry)
	}
}

// 最终 5xx（所有候选耗尽、未写出任何内容）必须透传上游原始错误体：此前只写状态不写体，
// 客户端拿到空 200，SDK 解析不出任何错误内容。502 的上游 body 原样到达客户端。
func TestFinishFailedRequestPassesThrough5xxBody(t *testing.T) {
	previous := payloadLogState
	defer func() { payloadLogState = previous }()
	dir := t.TempDir()
	InitPayloadLog(true, dir, 3)
	writer := httptest.NewRecorder()
	response := []byte(`{"error":"gateway failure"}`)
	(&sRelay{}).finishFailedRequest(writer, "afreq_5xx", Candidate{}, "public", "/responses", true, 1, nil, attemptResult{status: 502, body: response}, nil)
	if writer.Code != 502 || !bytes.Equal(writer.Body.Bytes(), response) {
		t.Fatalf("5xx passthrough changed: status=%d body=%q", writer.Code, writer.Body.Bytes())
	}
	entry := waitFailedPayload(t, dir, "afreq_5xx")
	if entry.HTTPStatus != 502 || !jsonEqual(entry.Response, response) {
		t.Fatalf("5xx payload changed: %+v", entry)
	}
}

// 上游错误体不是错误信封形状时（如 workbuddy 内容风控的 {"code":11128,...}，没有 error
// 字段）必须包装成标准 OpenAI 错误信封再透传：标准 SDK 与客户端错误分类层都依赖
// error.message 渲染错误，没有 error 字段的 body 表现为「有状态码但看不到任何错误内容」。
// 已是错误形状的原样透传；空 body 回退到带状态码的合成文案。
func TestClientVisibleErrorBodyWrapsNonEnvelopeBodies(t *testing.T) {
	cases := []struct {
		name    string
		result  attemptResult
		want    string
		contain string
	}{
		{
			name:   "workbuddy 风控体包装为标准信封",
			result: attemptResult{status: 400, body: []byte(`{"code": 11128, "msg": "request illegal", "requestId": "d0013ff2"}`)},
			want:   `{"error":{"message":"error: code=11128 msg=\"request illegal\" requestId=\"d0013ff2\"","type":"upstream_error"}}`,
		},
		{
			name:   "非 JSON 错误体回退到状态码文案（与 upstreamerror.Message 脱敏语义一致）",
			result: attemptResult{status: 502, body: []byte(`<html>upstream crashed</html>`)},
			want:   `{"error":{"message":"upstream returned HTTP 502","type":"upstream_error"}}`,
		},
		{
			name:   "已是标准信封原样透传",
			result: attemptResult{status: 400, body: []byte(`{"error":{"message":"original upstream failure","code":11128}}`)},
			want:   `{"error":{"message":"original upstream failure","code":11128}}`,
		},
		{
			name:   "字符串 error 字段同样透传",
			result: attemptResult{status: 502, body: []byte(`{"error":"gateway failure"}`)},
			want:   `{"error":"gateway failure"}`,
		},
		{
			name:   "空 body 回退到 errorMessage",
			result: attemptResult{status: 400, errorMessage: "error: code=11128 msg=\"request illegal\""},
			want:   `{"error":{"message":"error: code=11128 msg=\"request illegal\"","type":"upstream_error"}}`,
		},
		{
			name:   "空 body 无错误文案回退到状态码",
			result: attemptResult{status: 400},
			want:   `{"error":{"message":"upstream returned HTTP 400","type":"upstream_error"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clientVisibleErrorBody(tc.result)
			if tc.want != "" && !jsonEqual(got, []byte(tc.want)) {
				t.Fatalf("clientVisibleErrorBody = %s, want %s", got, tc.want)
			}
			if tc.contain != "" && !bytes.Contains(got, []byte(tc.contain)) {
				t.Fatalf("clientVisibleErrorBody = %s, want it to contain %q", got, tc.contain)
			}
		})
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
