package relay

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

func TestPrepareVideoRequestBodyMapsLegacyPromptWithoutChangingModel(t *testing.T) {
	body, err := prepareMiniMaxVideoRequestBody([]byte(`{"model":"minimax-h3","prompt":"A ferry crossing a quiet lake","duration":5,"resolution":"2K","ratio":"16:9"}`), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "minimax-h3" {
		t.Fatalf("model = %#v, want minimax-h3", payload["model"])
	}
	if _, exists := payload["prompt"]; exists {
		t.Fatalf("legacy prompt must be converted: %#v", payload)
	}
	content, ok := payload["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", payload["content"])
	}
	item := content[0].(map[string]any)
	if item["type"] != "text" || item["text"] != "A ferry crossing a quiet lake" {
		t.Fatalf("content item = %#v", item)
	}
}

func TestPrepareVideoRequestBodyPreservesNonMiniMaxPayload(t *testing.T) {
	original := []byte(`{"model":"other","prompt":"test","custom":true}`)
	adapter := videoAdapter{code: channeltype.VideoAdapterOpenAI}
	body, err := adapter.prepareBody(original, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(original) {
		t.Fatalf("payload = %s, want %s", body, original)
	}
}

func TestVideoAdapterURLsPerProtocol(t *testing.T) {
	cases := []struct {
		adapter       string
		baseURL       string
		legacyCreate  string
		openAICreate  string
		legacyQuery   string
		openAIQuery   string
	}{
		{channeltype.VideoAdapterOpenAI, "https://gateway.example/v1",
			"https://gateway.example/v1/video/generations",
			"https://gateway.example/v1/videos",
			"https://gateway.example/v1/video/generations/task_1",
			"https://gateway.example/v1/videos/task_1"},
		{channeltype.VideoAdapterMiniMax, "https://api.minimax.io/v1",
			"https://api.minimax.io/v2/video_generation",
			"https://api.minimax.io/v2/video_generation",
			"https://api.minimax.io/v2/query/video_generation/task_1",
			"https://api.minimax.io/v2/query/video_generation/task_1"},
		{channeltype.VideoAdapterVolcengineArk, "https://ark.cn-beijing.volces.com/api/v3",
			"https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
			"https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
			"https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/task_1",
			"https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/task_1"},
	}
	for _, tc := range cases {
		adapter := videoAdapter{code: tc.adapter}
		candidate := Candidate{ChannelType: tc.adapter, BaseURL: tc.baseURL}
		if got := adapter.createURL(candidate, legacyVideoAPI); got != tc.legacyCreate {
			t.Fatalf("%s legacy create URL = %q, want %q", tc.adapter, got, tc.legacyCreate)
		}
		if got := adapter.createURL(candidate, openAIVideoAPI); got != tc.openAICreate {
			t.Fatalf("%s OpenAI create URL = %q, want %q", tc.adapter, got, tc.openAICreate)
		}
		if got := adapter.retrieveURL(candidate, "task_1", legacyVideoAPI); got != tc.legacyQuery {
			t.Fatalf("%s legacy query URL = %q, want %q", tc.adapter, got, tc.legacyQuery)
		}
		if got := adapter.retrieveURL(candidate, "task_1", openAIVideoAPI); got != tc.openAIQuery {
			t.Fatalf("%s OpenAI query URL = %q, want %q", tc.adapter, got, tc.openAIQuery)
		}
	}
}

func TestArkVideoResponseURLExtractionAndRewrite(t *testing.T) {
	body := []byte(`{"id":"cgt-1","status":"succeeded","content":{"video_url":"https://ark.example/result.mp4"}}`)
	if got := arkVideoResponseURL(body); got != "https://ark.example/result.mp4" {
		t.Fatalf("ark video URL = %q", got)
	}
	rewritten := rewriteArkVideoResponseURL(body, "/v1/videos/cgt-1/content")
	if got := gjson.GetBytes(rewritten, "content.video_url").String(); got != "/v1/videos/cgt-1/content" {
		t.Fatalf("rewritten ark URL = %q", got)
	}
	if got := arkVideoResponseURL([]byte(`{"status":"running"}`)); got != "" {
		t.Fatalf("running task URL = %q", got)
	}
}

func TestVideoRequestedModelSupportsJSONAndMultipart(t *testing.T) {
	model, err := videoRequestedModel([]byte(`{"model":"sora-2-pro","prompt":"test"}`), "application/json")
	if err != nil || model != "sora-2-pro" {
		t.Fatalf("JSON model = %q, err = %v", model, err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err = writer.WriteField("prompt", "test"); err != nil {
		t.Fatal(err)
	}
	if err = writer.WriteField("model", "sora-2"); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	model, err = videoRequestedModel(body.Bytes(), writer.FormDataContentType())
	if err != nil || model != "sora-2" {
		t.Fatalf("multipart model = %q, err = %v", model, err)
	}
}

func TestVideoResponseIDAcceptsBothUpstreamShapes(t *testing.T) {
	if got := videoResponseID([]byte(`{"task_id":"task_1"}`), legacyVideoAPI); got != "task_1" {
		t.Fatalf("legacy id = %q", got)
	}
	if got := videoResponseID([]byte(`{"id":"video_1"}`), openAIVideoAPI); got != "video_1" {
		t.Fatalf("OpenAI id = %q", got)
	}
	if got := videoResponseID([]byte(`{"task_id":"task_2"}`), openAIVideoAPI); got != "task_2" {
		t.Fatalf("fallback id = %q", got)
	}
}

func TestVideoResourceURL(t *testing.T) {
	candidate := Candidate{BaseURL: "https://gateway.example/v1/"}
	if got := videoResourceURL(candidate, "video_123", false); got != "https://gateway.example/v1/videos/video_123" {
		t.Fatalf("retrieve URL = %q", got)
	}
	if got := videoResourceURL(candidate, "video_123", true); got != "https://gateway.example/v1/videos/video_123/content" {
		t.Fatalf("content URL = %q", got)
	}
}

func TestMiniMaxVideoContentPathsAndResponseURLRewrite(t *testing.T) {
	if got := videoContentPath("task_123", legacyVideoAPI); got != "/v1/video/generations/task_123/content" {
		t.Fatalf("legacy content path = %q", got)
	}
	if got := videoContentPath("video_123", openAIVideoAPI); got != "/v1/videos/video_123/content" {
		t.Fatalf("OpenAI content path = %q", got)
	}
	body := rewriteMiniMaxVideoResponseURL([]byte(`{"status":"completed","url":"/resources/gateway/result.mp4"}`), "/v1/video/generations/task_123/content")
	if got := gjson.GetBytes(body, "url").String(); got != "/v1/video/generations/task_123/content" {
		t.Fatalf("rewritten URL = %q", got)
	}
}

func TestResolveMiniMaxVideoResourceURL(t *testing.T) {
	resource, err := resolveMiniMaxVideoResourceURL("https://gateway.example/v1", "/resources/gateway/result.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if resource != "https://gateway.example/resources/gateway/result.mp4" {
		t.Fatalf("relative resource URL = %q", resource)
	}
	resource, err = resolveMiniMaxVideoResourceURL("https://gateway.example/v1", "https://cdn.example/result.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if resource != "https://cdn.example/result.mp4" {
		t.Fatalf("absolute resource URL = %q", resource)
	}
	if _, err = resolveMiniMaxVideoResourceURL("https://gateway.example/v1", "file:///tmp/result.mp4"); err == nil {
		t.Fatal("expected non-HTTP resource URL to be rejected")
	}
	if !sameUpstreamOrigin("https://gateway.example/v1", "https://gateway.example/resources/result.mp4") {
		t.Fatal("expected same upstream origin")
	}
	if sameUpstreamOrigin("https://gateway.example/v1", "https://cdn.example/result.mp4") {
		t.Fatal("expected different upstream origin")
	}
	candidate := Candidate{ProxyURLCipher: "encrypted-proxy"}
	if !sameUpstreamOrigin(candidate.BaseURL, resource) {
		candidate.ProxyURLCipher = ""
	}
	if candidate.ProxyURLCipher != "" {
		t.Fatal("cross-origin resource must not use the channel proxy")
	}
}

func TestNormalizedVideoUpstreamResponseAddsStructuredErrorForEmptyFailure(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Trace-Id", "upstream-trace-123")
	status, body, responseHeaders, err := normalizedVideoUpstreamResponse(400, nil, headers, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != 400 {
		t.Fatalf("status = %d", status)
	}
	if responseHeaders.Get("Trace-Id") != "upstream-trace-123" {
		t.Fatalf("Trace-Id = %q", responseHeaders.Get("Trace-Id"))
	}
	if responseHeaders.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", responseHeaders.Get("Content-Type"))
	}
	if responseHeaders.Get("X-AiFerry-Upstream-Status") != "400" {
		t.Fatalf("X-AiFerry-Upstream-Status = %q", responseHeaders.Get("X-AiFerry-Upstream-Status"))
	}
	if got := string(body); got != `{"error":{"message":"Upstream video provider returned HTTP 400 without an error response body","type":"upstream_error"}}` {
		t.Fatalf("body = %s", got)
	}
}

func TestNormalizedVideoUpstreamResponseKeepsNonEmptyUpstreamError(t *testing.T) {
	original := []byte(`{"error":{"message":"provider rejected ratio","type":"invalid_request_error"}}`)
	headers := http.Header{"Content-Type": []string{"application/json"}}
	status, body, responseHeaders, err := normalizedVideoUpstreamResponse(400, original, headers, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != 400 || string(body) != string(original) {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if responseHeaders.Get("X-AiFerry-Upstream-Status") != "" {
		t.Fatalf("unexpected upstream status header: %q", responseHeaders.Get("X-AiFerry-Upstream-Status"))
	}
}
