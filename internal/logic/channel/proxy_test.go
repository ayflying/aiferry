package channel

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// 多行代理配置按换行拆分：去空白、丢空行，单行历史数据原样返回一行。
func TestSplitProxyLines(t *testing.T) {
	got := splitProxyLines("  http://p1:8080 \n\nsocks5://u:p@p2:1080\n")
	if len(got) != 2 || got[0] != "http://p1:8080" || got[1] != "socks5://u:p@p2:1080" {
		t.Fatalf("splitProxyLines = %#v", got)
	}
	single := splitProxyLines("http://p1:8080")
	if len(single) != 1 || single[0] != "http://p1:8080" {
		t.Fatalf("single line should stay one entry: %#v", single)
	}
	if empty := splitProxyLines("  \n \n"); len(empty) != 0 {
		t.Fatalf("blank-only input should yield no entries: %#v", empty)
	}
}

// 模运算配对：2 个代理、5 把密钥 → 1-1,2-2,3-1,4-2,5-1。
func TestProxyIndexOfModuloMapping(t *testing.T) {
	want := []int{0, 1, 0, 1, 0}
	for i, expected := range want {
		if got := ProxyIndexOf(uint(i+1), 2); got != expected {
			t.Fatalf("ordinal %d: ProxyIndexOf = %d, want %d", i+1, got, expected)
		}
	}
	if got := ProxyIndexOf(3, 0); got != -1 {
		t.Fatalf("no proxies should be -1, got %d", got)
	}
	if got := ProxyIndexOf(0, 3); got != 0 {
		t.Fatalf("ordinal 0 should fall back to first proxy, got %d", got)
	}
}

// 密钥基线由序号决定；Advance 只动自己这份计划，别的密钥映射不受影响；
// 尝试次数超过代理总数后停止顺延。
func TestCredentialProxiesBaselineAndAdvance(t *testing.T) {
	urls := []string{"http://p1:8080", "http://p2:8080"}
	key1 := newCredentialProxies(urls, 1)
	key2 := newCredentialProxies(urls, 2)
	key3 := newCredentialProxies(urls, 3)
	if key1.Current() != urls[0] || key2.Current() != urls[1] || key3.Current() != urls[0] {
		t.Fatalf("baseline mapping wrong: %q %q %q", key1.Current(), key2.Current(), key3.Current())
	}
	// 只有 key1 顺延：key1→p2，key2/key3 不动。
	if !key1.Advance() {
		t.Fatal("first advance should succeed")
	}
	if key1.Current() != urls[1] || key2.Current() != urls[1] || key3.Current() != urls[0] {
		t.Fatalf("advance must not move other keys: %q %q %q", key1.Current(), key2.Current(), key3.Current())
	}
	// 2 个代理已各试一次，再次顺延应拒绝（不无限转圈）。
	if key1.Advance() {
		t.Fatal("advance beyond total should stop")
	}
	if key1.Current() != urls[1] {
		t.Fatalf("stopped advance must keep current proxy, got %q", key1.Current())
	}
	// 无代理计划：Current 空、Advance 拒绝。
	direct := newCredentialProxies(nil, 5)
	if direct.Total() != 0 || direct.Current() != "" || direct.Advance() {
		t.Fatalf("empty plan should be inert: %+v", direct)
	}
}

// doViaProxies：传输错误时顺延到下一个代理重试；拿到非 407 响应立即返回
// （上游失败不换代理）；407 视为代理失败参与顺延；单代理失败直接收尾。
func TestDoViaProxiesRotatesOnlyOnProxyLayerFailure(t *testing.T) {
	urls := []string{"http://p1:8080", "http://p2:8080"}
	clientFor := func(p *CredentialProxies) (*http.Client, error) { return http.DefaultClient, nil }

	t.Run("transport error advances to next proxy", func(t *testing.T) {
		plan := newCredentialProxies(urls, 1)
		var used []string
		resets := 0
		resp, err := doViaProxies(plan, clientFor, func() error { resets++; return nil }, func(_ *http.Client) (*http.Response, error) {
			used = append(used, plan.Current())
			if len(used) == 1 {
				return nil, errors.New("dial tcp: connection refused")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})
		if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("retry through second proxy should succeed: resp=%v err=%v", resp, err)
		}
		if len(used) != 2 || used[0] != urls[0] || used[1] != urls[1] {
			t.Fatalf("proxy order = %#v, want [%s %s]", used, urls[0], urls[1])
		}
		if resets < 2 {
			t.Fatalf("request body must be reset before every attempt, resets=%d", resets)
		}
	})

	t.Run("upstream response does not rotate", func(t *testing.T) {
		plan := newCredentialProxies(urls, 1)
		calls := 0
		resp, err := doViaProxies(plan, clientFor, nil, func(_ *http.Client) (*http.Response, error) {
			calls++
			// 500 来自上游而非代理：不能触发换代理。
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("boom"))}, nil
		})
		if err != nil || resp == nil || resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("upstream 500 must be returned as-is: resp=%v err=%v", resp, err)
		}
		if calls != 1 {
			t.Fatalf("upstream failure must not rotate, calls=%d", calls)
		}
		if plan.Current() != urls[0] {
			t.Fatalf("baseline must stay, got %q", plan.Current())
		}
	})

	t.Run("407 advances then succeeds", func(t *testing.T) {
		plan := newCredentialProxies(urls, 1)
		calls := 0
		resp, err := doViaProxies(plan, clientFor, nil, func(_ *http.Client) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusProxyAuthRequired, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})
		if err != nil || resp == nil || resp.StatusCode != http.StatusOK || calls != 2 {
			t.Fatalf("407 should rotate once: calls=%d resp=%v err=%v", calls, resp, err)
		}
	})

	t.Run("single proxy failure returns immediately", func(t *testing.T) {
		plan := newCredentialProxies([]string{urls[0]}, 1)
		calls := 0
		_, err := doViaProxies(plan, clientFor, nil, func(_ *http.Client) (*http.Response, error) {
			calls++
			return nil, errors.New("proxy down")
		})
		if err == nil || calls != 1 {
			t.Fatalf("single proxy must not loop: calls=%d err=%v", calls, err)
		}
	})
}
