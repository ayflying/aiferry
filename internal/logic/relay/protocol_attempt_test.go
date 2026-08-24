package relay

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

func TestPreferredProtocolPlanUsesUpstreamModel(t *testing.T) {
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := preferredProtocolPlan(test.clientEndpoint, test.candidate)
			if plan.UpstreamEndpoint() != test.upstreamEndpoint || plan.Converts() != test.converts {
				t.Fatalf("plan = endpoint %q, converts %t; want endpoint %q, converts %t", plan.UpstreamEndpoint(), plan.Converts(), test.upstreamEndpoint, test.converts)
			}
		})
	}
}
