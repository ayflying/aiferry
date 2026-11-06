package relay

import (
	"net/http"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// TestResponseHeadersDropsCompressionHeaders 验证回写客户端的响应头不再携带
// 上游的长度与编码声明：net/http 读取上游体时已按 Content-Encoding 自动解压，
// 沿用上游 Content-Length（压缩后长度）会让客户端按错误长度截断或报错。
func TestResponseHeadersDropsCompressionHeaders(t *testing.T) {
	upstream := http.Header{
		"Content-Type":      []string{"application/json"},
		"Content-Length":    []string{"128"},
		"Content-Encoding":  []string{"gzip"},
		"Transfer-Encoding": []string{"chunked"},
		"X-Request-Id":      []string{"req-1"},
	}
	for _, plan := range []protocol.Plan{protocol.DirectPlan("/chat/completions"), protocol.PreferredPlan("/chat/completions", "gpt-4o")} {
		headers := responseHeaders(upstream, plan)
		for _, name := range []string{"Content-Length", "Content-Encoding", "Transfer-Encoding"} {
			if value := headers.Get(name); value != "" {
				t.Fatalf("%s kept %s = %q", plan.UpstreamEndpoint(), name, value)
			}
		}
		if headers.Get("Content-Type") == "" {
			t.Fatalf("%s: Content-Type dropped", plan.UpstreamEndpoint())
		}
		if headers.Get("X-Request-Id") != "req-1" {
			t.Fatalf("%s: business header X-Request-Id dropped", plan.UpstreamEndpoint())
		}
	}
}

// TestDropClientResponseHeader 验证控制器层复制上游响应头时的过滤规则。
func TestDropClientResponseHeader(t *testing.T) {
	for _, name := range []string{"Connection", "Content-Length", "Content-Encoding", "Transfer-Encoding", "Keep-Alive"} {
		if !dropClientResponseHeader(name) {
			t.Fatalf("hop/length header %s should be dropped", name)
		}
	}
	for _, name := range []string{"Content-Type", "Content-Disposition", "X-Request-Id", "Cache-Control"} {
		if dropClientResponseHeader(name) {
			t.Fatalf("business header %s should be kept", name)
		}
	}
}