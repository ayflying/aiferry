package app

import (
	"net/http"
	"testing"
)

func TestNewProxyHTTPClientRejectsUnsupportedProxyURL(t *testing.T) {
	if _, err := NewProxyHTTPClient(http.DefaultClient, "ftp://proxy.example:1080"); err == nil {
		t.Fatal("unsupported proxy URL should be rejected")
	}
	if _, err := NewProxyHTTPClient(http.DefaultClient, "socks5://proxy.example"); err == nil {
		t.Fatal("proxy URL without port should be rejected")
	}
}

func TestNewProxyHTTPClientKeepsDirectClientWithoutProxy(t *testing.T) {
	client, err := NewProxyHTTPClient(http.DefaultClient, "")
	if err != nil || client != http.DefaultClient {
		t.Fatalf("unexpected direct client: %v %p", err, client)
	}
}

func TestNewProxyHTTPClientUsesHTTPProxy(t *testing.T) {
	client, err := NewProxyHTTPClient(http.DefaultClient, "http://user:pass@proxy.example:8080")
	if err != nil {
		t.Fatalf("create HTTP proxy client: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/models", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("resolve HTTP proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://user:pass@proxy.example:8080" {
		t.Fatalf("proxy URL = %v", proxyURL)
	}
}

func TestNewProxyHTTPClientUsesSOCKS5Proxy(t *testing.T) {
	client, err := NewProxyHTTPClient(http.DefaultClient, "socks5://user:pass@proxy.example:1080")
	if err != nil {
		t.Fatalf("create SOCKS5 proxy client: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil {
		t.Fatalf("unexpected SOCKS5 transport: %#v", transport)
	}
}
