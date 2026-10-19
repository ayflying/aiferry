package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tidwall/gjson"
	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

// 覆盖真实请求构造链路，确保不再回填、补空串、归一 null 或改写方言字段。
func TestAttemptPreservesClientReasoningHistory(t *testing.T) {
	for _, field := range []string{"", `,"reasoning_content":null`, `,"reasoning_content":""`, `,"reasoning_content":"  原始思考  "`, `,"reasoning":"  方言思考  "`} {
		t.Run(field, func(t *testing.T) {
			body := []byte(`{"model":"deepseek-flash","messages":[{"role":"user","content":"测试"},{"role":"assistant","content":""` + field + `,"tool_calls":[{"id":"call_probe","type":"function","function":{"name":"probe","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_probe","content":"OK"}]}`)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				for _, name := range []string{"reasoning_content", "reasoning", "tool_calls.0.id", "tool_calls.0.type", "tool_calls.0.function.name", "tool_calls.0.function.arguments"} {
					path := "messages.1." + name
					before, after := gjson.GetBytes(body, path), gjson.GetBytes(received, path)
					if before.Exists() != after.Exists() || before.Raw != after.Raw {
						t.Errorf("%s 被改写：before=%s after=%s", path, before.Raw, after.Raw)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
			}))
			defer upstream.Close()
			secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
			if err != nil {
				t.Fatal(err)
			}
			appSvc := &app.Service{Secrets: secrets, HTTP: upstream.Client()}
			svc := &sRelay{app: appSvc, channels: channel.New(appSvc, channeltype.New(&config.BuiltinRegistry{}, nil), nil, nil, nil, nil, nil, nil)}
			candidate := Candidate{BaseURL: upstream.URL, UpstreamName: "deepseek-flash"}
			settings := adminapi.SystemResilienceSettingsInput{NonStreamTimeoutSeconds: 10}
			plan := protocol.PreferredPlan(protocol.ChatCompletionsEndpoint, "deepseek-flash")
			result, _, err := svc.attemptWithProtocol(context.Background(), httptest.NewRecorder(), http.Header{}, body, candidate, false, 1, 1, settings, channel.AdvancedConfig{}, plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.status != http.StatusOK {
				t.Fatalf("status=%d", result.status)
			}
		})
	}
}
