package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeProxyRejectsEmptyAndInvalidURL(t *testing.T) {
	empty := probeProxy(http.DefaultClient, "   ")
	if empty.OK || empty.Message == "" {
		t.Fatalf("empty proxy should be rejected with message: %+v", empty)
	}
	invalid := probeProxy(http.DefaultClient, "not-a-url")
	if invalid.OK {
		t.Fatalf("invalid proxy URL should be rejected: %+v", invalid)
	}
	unsupported := probeProxy(http.DefaultClient, "ftp://proxy.example:1080")
	if unsupported.OK {
		t.Fatalf("unsupported scheme should be rejected: %+v", unsupported)
	}
}

// 假 HTTP 代理返回 204：探测应判定可用并带回时延。
func TestProbeProxyViaStubProxy(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer stub.Close()

	result := probeProxy(http.DefaultClient, stub.URL)
	if !result.OK {
		t.Fatalf("stub proxy should be reachable: %+v", result)
	}
	if result.LatencyMs < 0 {
		t.Fatalf("latency should be recorded: %+v", result)
	}
}

// 假代理返回 407：认证失败，判定不可用。
func TestProbeProxyReportsProxyAuthRequired(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer stub.Close()

	result := probeProxy(http.DefaultClient, stub.URL)
	if result.OK {
		t.Fatalf("407 must be treated as failure: %+v", result)
	}
}

// 代理地址指向不存在的端口：连接失败，判定不可用。
func TestProbeProxyUnreachableProxy(t *testing.T) {
	result := probeProxy(http.DefaultClient, "http://127.0.0.1:1")
	if result.OK {
		t.Fatalf("unreachable proxy must fail: %+v", result)
	}
}
