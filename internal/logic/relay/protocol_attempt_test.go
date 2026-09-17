package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

func TestPreferredProtocolPlanUsesUpstreamModel(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{}

	tests := []struct {
		name             string
		clientEndpoint   string
		candidate        Candidate
		upstreamEndpoint string
		converts         bool
	}{
		{
			name:           "GPT upstream uses Responses",
			clientEndpoint: protocol.ChatCompletionsEndpoint,
			candidate: Candidate{
				PublicName:   "custom-gpt-name",
				UpstreamName: "gpt-5.6-terra",
			},
			upstreamEndpoint: protocol.ResponsesEndpoint,
			converts:         true,
		},
		{
			name:           "GPT public alias keeps non-GPT upstream on Chat",
			clientEndpoint: protocol.ChatCompletionsEndpoint,
			candidate: Candidate{
				PublicName:   "gpt-5.6-luna",
				UpstreamName: "qwen3-235b-a22b-instruct-2507",
			},
			upstreamEndpoint: protocol.ChatCompletionsEndpoint,
			converts:         false,
		},
		{
			name:           "non-GPT upstream converts Codex Responses requests to Chat",
			clientEndpoint: protocol.ResponsesEndpoint,
			candidate: Candidate{
				PublicName:   "gpt-5.6-luna",
				UpstreamName: "deepseek-v4-pro",
			},
			upstreamEndpoint: protocol.ChatCompletionsEndpoint,
			converts:         true,
		},
		{
			name:           "GPT upstream keeps Responses requests native",
			clientEndpoint: protocol.ResponsesEndpoint,
			candidate: Candidate{
				PublicName:   "custom-model",
				UpstreamName: "gpt-5.6-terra",
			},
			upstreamEndpoint: protocol.ResponsesEndpoint,
			converts:         false,
		},
		{
			name:           "Zhipu Responses address uses native Responses for GLM",
			clientEndpoint: protocol.ResponsesEndpoint,
			candidate: Candidate{
				ChannelType:  "zhipu",
				BaseURL:      "https://open.bigmodel.cn/api/v1",
				UpstreamName: "glm-5.3",
			},
			upstreamEndpoint: protocol.ResponsesEndpoint,
			converts:         false,
		},
		{
			name:           "Zhipu Responses address converts Chat for GLM",
			clientEndpoint: protocol.ChatCompletionsEndpoint,
			candidate: Candidate{
				ChannelType:  "zhipu",
				BaseURL:      "https://open.bigmodel.cn/api/v1/",
				UpstreamName: "glm-5.3",
			},
			upstreamEndpoint: protocol.ResponsesEndpoint,
			converts:         true,
		},
	}

	service := &sRelay{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := service.preferredProtocolPlan(context.Background(), test.clientEndpoint, test.candidate, true, settings)
			if plan.UpstreamEndpoint() != test.upstreamEndpoint || plan.Converts() != test.converts {
				t.Fatalf("plan = endpoint %q, converts %t; want endpoint %q, converts %t", plan.UpstreamEndpoint(), plan.Converts(), test.upstreamEndpoint, test.converts)
			}
		})
	}
}

// 协议回退（首选端点 404 → 回退另一端点）是同一候选内的第二次真实上游调用：
// attempt() 必须把首跳的失败登记进 precedingFlow，供调用方并入调用流程与尝试计数。
func TestAttemptRecordsProtocolFallbackFlow(t *testing.T) {
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	var responsesCalls, chatCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/responses":
			responsesCalls++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"responses endpoint not supported"}}`))
		case "/chat/completions":
			chatCalls++
			_, _ = w.Write([]byte(`{"id":"cmpl-fallback","object":"chat.completion","model":"gpt-5.6-terra","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
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
		ChannelID:    7,
		ChannelName:  "ch-fallback-test",
		ChannelType:  "probe_fallback",
		BaseURL:      upstream.URL,
		UpstreamName: "gpt-5.6-terra",
	}
	body := []byte(`{"model":"gpt-5.6-terra","messages":[{"role":"user","content":"hi"}]}`)
	result, _, attemptErr := relay.attempt(context.Background(), nil, http.Header{}, protocol.ChatCompletionsEndpoint, body, candidate, false, 11, 42, settings, nil)
	if attemptErr != nil {
		t.Fatal(attemptErr)
	}
	if result.status != http.StatusOK {
		t.Fatalf("fallback should succeed with 200, got %d: %s", result.status, result.errorMessage)
	}
	if responsesCalls != 1 || chatCalls != 1 {
		t.Fatalf("want one call per endpoint, got /responses=%d /chat/completions=%d", responsesCalls, chatCalls)
	}
	if len(result.precedingFlow) != 1 {
		t.Fatalf("precedingFlow = %d steps; want 1 (the failed /responses hop)", len(result.precedingFlow))
	}
	step := result.precedingFlow[0]
	if step.Endpoint != protocol.ResponsesEndpoint || step.Status == nil || *step.Status != http.StatusNotFound {
		t.Fatalf("first hop should keep /responses 404: %+v", step)
	}
	if step.ChannelName != candidate.ChannelName {
		t.Fatalf("first hop channel = %q; want %q", step.ChannelName, candidate.ChannelName)
	}
	if result.upstreamEndpoint != protocol.ChatCompletionsEndpoint {
		t.Fatalf("final hop endpoint = %q; want %q", result.upstreamEndpoint, protocol.ChatCompletionsEndpoint)
	}
}
