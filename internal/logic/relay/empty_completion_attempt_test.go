package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

// Codex 生产事故（2026-10-29 17:53，deepseek-flash）：Responses 客户端经
// responses_to_chat 转换打到只回「2xx 但 choices 为空」的上游，网关把空壳原样
// 转成 status=completed、output=[] 的 Responses 响应，Codex 只能把它当结构化
// 错误打印：error: created_at=... id="" model="" object="response" output=[]
// status="completed"。
//
// 修复后这类空回包必须判为上游失败（502），交给候选循环换渠道重试，
// 而不是当成功响应交付给客户端。
func TestEmptyUpstreamCompletionIsRejectedAsBadGateway(t *testing.T) {
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	var chatCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			chatCalls++
			// 上游 200，但没有任何 choices：历史 bug 就是在这里被转成空 output。
			_, _ = w.Write([]byte(`{"id":"","model":"","choices":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
		}
	}))
	defer upstream.Close()

	appSvc := &app.Service{Secrets: secrets, HTTP: upstream.Client()}
	relay := &sRelay{
		app:      appSvc,
		channels: channel.New(appSvc, channeltype.New(&config.BuiltinRegistry{}, nil), nil, nil, nil, nil, nil, nil),
		prices:   pricingcache.New(),
	}
	settings := adminapi.SystemResilienceSettingsInput{ProtocolConversionEnabled: true, NonStreamTimeoutSeconds: 30}
	candidate := Candidate{
		ChannelID:    9,
		ChannelName:  "ch-empty-completion",
		ChannelType:  "probe_empty",
		BaseURL:      upstream.URL,
		UpstreamName: "deepseek-flash",
	}
	body := []byte(`{"model":"deepseek-flash","input":[{"role":"user","content":"hi"}]}`)
	result, _, attemptErr := relay.attempt(context.Background(), httptest.NewRecorder(), http.Header{}, protocol.ResponsesEndpoint, body, candidate, false, 11, 42, settings, nil)
	if attemptErr != nil {
		t.Fatal(attemptErr)
	}
	if chatCalls != 1 {
		t.Fatalf("want exactly one upstream call, got %d", chatCalls)
	}
	if result.status != http.StatusBadGateway {
		t.Fatalf("empty upstream completion must be rejected as 502, got %d: %s", result.status, string(result.body))
	}
	// 交给客户端的必须是可读错误，绝不能是 status=completed、output=[] 的响应壳。
	if gjson.GetBytes(result.body, "response").Exists() {
		t.Fatalf("must not return a Responses shell for an empty completion: %s", string(result.body))
	}
	if message := gjson.GetBytes(result.body, "error.message").String(); message == "" {
		t.Fatalf("rejected response needs a readable error message: %s", string(result.body))
	}
	if result.errorMessage == "" {
		t.Fatal("attempt result needs an error message for usage logging")
	}
}
