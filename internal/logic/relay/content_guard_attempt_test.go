package relay

import (
	"context"
	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/secret"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 验证真实 writer 边界，不能只以 Observe 返回 nil 推断候选可以重试。
func TestContentGuardAttemptRetainsRetryWindow(t *testing.T) {
	for _, ending := range []string{"", "data: [DONE]\n\n", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"} {
		t.Run(ending, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"<analysis>草稿\"}}]}\n\n" + ending))
			}))
			defer upstream.Close()
			secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
			if err != nil {
				t.Fatal(err)
			}
			appSvc := &app.Service{Secrets: secrets, HTTP: upstream.Client()}
			svc := &sRelay{app: appSvc, channels: channel.New(appSvc, channeltype.New(&config.BuiltinRegistry{}, nil), nil, nil, nil, nil, nil, nil)}
			writer := httptest.NewRecorder()
			result, handled, err := svc.attemptWithProtocol(context.Background(), writer, http.Header{}, []byte(`{"model":"test","stream":true,"messages":[{"role":"user","content":"测试"}]}`), Candidate{BaseURL: upstream.URL, UpstreamName: "test"}, true, 1, 1, adminapi.SystemResilienceSettingsInput{StreamFirstByteTimeoutSeconds: 10, StreamIdleTimeoutSeconds: 10}, channel.AdvancedConfig{}, protocol.PreferredPlan(protocol.ChatCompletionsEndpoint, "test"), nil)
			if err != nil {
				t.Fatal(err)
			}
			if handled || result.wroteBytes || writer.Body.Len() != 0 || result.status != http.StatusBadGateway {
				t.Fatalf("重试窗口丢失 handled=%v wrote=%v body=%s status=%d", handled, result.wroteBytes, writer.Body.String(), result.status)
			}
		})
	}
}
