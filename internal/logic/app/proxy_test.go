package app

import (
	"net/http"
	"testing"
)

func TestNewSOCKS5HTTPClientRejectsUnsupportedProxyURL(t *testing.T) {
	if _, err := NewSOCKS5HTTPClient(http.DefaultClient, "http://proxy.example:1080"); err == nil {
		t.Fatal("HTTP proxy URL should be rejected")
	}
	if _, err := NewSOCKS5HTTPClient(http.DefaultClient, "socks5://proxy.example"); err == nil {
		t.Fatal("proxy URL without port should be rejected")
	}
}

func TestNewSOCKS5HTTPClientKeepsDirectClientWithoutProxy(t *testing.T) {
	client, err := NewSOCKS5HTTPClient(http.DefaultClient, "")
	if err != nil || client != http.DefaultClient {
		t.Fatalf("unexpected direct client: %v %p", err, client)
	}
}
