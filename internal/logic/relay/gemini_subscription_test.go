package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

func TestGeminiSubscriptionProxyCannotBeBypassed(t *testing.T) {
	relay := proxyTestRelay(t, http.DefaultClient)
	req := httptest.NewRequest(http.MethodPost, "https://upstream.invalid", nil)
	for _, direct := range []bool{false, true} {
		resp, err := relay.doViaProxy(context.Background(), req, Candidate{ChannelType: channel.GeminiSubscriptionChannelType, DirectHTTP: direct}, func(*http.Client) (*http.Response, error) {
			t.Fatal("request callback called without a channel proxy")
			return nil, nil
		})
		if resp != nil || err == nil || !strings.Contains(err.Error(), "必须配置代理") {
			t.Fatalf("response = %v, error = %v", resp, err)
		}
	}
}

func TestGeminiSubscriptionRejectsTokenUseOnOtherEndpoints(t *testing.T) {
	relay := &sRelay{}
	req := httptest.NewRequest(http.MethodPost, "https://upstream.invalid", nil)
	err := relay.applyEndpointAuthHeaders(context.Background(), req, Candidate{ChannelType: channel.GeminiSubscriptionChannelType}, "")
	if err == nil || req.Header.Get("Authorization") != "" {
		t.Fatalf("auth applied to untrusted endpoint: %v", err)
	}
}
