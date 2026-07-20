package iplocation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeIP(t *testing.T) {
	if actual := NormalizeIP("::ffff:203.0.113.8"); actual != "203.0.113.8" {
		t.Fatalf("unexpected normalized IP: %s", actual)
	}
	if actual := NormalizeIP("203.0.113.8:443"); actual != "203.0.113.8" {
		t.Fatalf("unexpected IPv4 connection address: %s", actual)
	}
	if actual := NormalizeIP("[2001:db8::8]:443"); actual != "2001:db8::8" {
		t.Fatalf("unexpected IPv6 connection address: %s", actual)
	}
	if actual := NormalizeIP("not-an-ip"); actual != "" {
		t.Fatalf("invalid IP should be blank: %s", actual)
	}
}

func TestFormatRegion(t *testing.T) {
	actual := formatRegion("中国|广东省|深圳市|电信|CN")
	if actual != "中国 / 广东省 / 深圳市 / 电信" {
		t.Fatalf("unexpected region: %s", actual)
	}
}

func TestNewServiceDoesNotBlockOnDatabaseDownload(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case requestStarted <- struct{}{}:
		default:
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan *Service, 1)
	go func() {
		result <- newService(ctx, server.Client(), t.TempDir(), []database{
			{name: v4DatabaseName, url: server.URL + "/v4"},
			{name: v6DatabaseName, url: server.URL + "/v6"},
		}, time.Minute)
	}()

	select {
	case service := <-result:
		if service.searcher.Load() != nil {
			t.Fatal("lookup should remain disabled until databases are downloaded")
		}
	case <-time.After(time.Second):
		t.Fatal("service startup blocked on IP location database download")
	}

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("background database download did not start")
	}
}
