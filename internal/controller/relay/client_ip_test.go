package relay

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/util/guid"
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

func TestVideoContentRoutesMatchPathParameters(t *testing.T) {
	const webRoot = "web"
	if err := os.Mkdir(webRoot, 0o700); err != nil {
		t.Fatalf("create test web root: %v", err)
	}
	defer os.RemoveAll(webRoot)
	if err := os.WriteFile(webRoot+"/index.html", []byte("ok"), 0o600); err != nil {
		t.Fatalf("write test index: %v", err)
	}
	s := g.Server(guid.S())
	s.SetDumpRouterMap(false)
	s.SetFileServerEnabled(false)
	s.Group("/v1", func(group *ghttp.RouterGroup) {
		group.GET("/video/generations/:task_id/content", func(r *ghttp.Request) {
			r.Response.Write("legacy:" + r.GetRouter("task_id").String())
		})
		group.GET("/videos/:video_id/content", func(r *ghttp.Request) {
			r.Response.Write("openai:" + r.GetRouter("video_id").String())
		})
	})
	s.Start()
	defer s.Shutdown()
	time.Sleep(100 * time.Millisecond)

	client := g.Client()
	client.SetPrefix(fmt.Sprintf("http://127.0.0.1:%d", s.GetListenedPort()))
	for path, want := range map[string]string{
		"/v1/video/generations/task_123/content": "legacy:task_123",
		"/v1/videos/video_456/content":           "openai:video_456",
	} {
		content := client.GetContent(context.Background(), path)
		if content != want {
			t.Fatalf("GET %s = %q, want %q", path, content, want)
		}
	}
}
