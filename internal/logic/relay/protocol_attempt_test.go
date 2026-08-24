package relay

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

func TestPreferredProtocolPlanUsesUpstreamModel(t *testing.T) {
	tests := []struct {
		name             string
		candidate        Candidate
		upstreamEndpoint string
		converts         bool
	}{
		{
			name: "GPT upstream uses Responses",
			candidate: Candidate{
				PublicName:   "custom-gpt-name",
				UpstreamName: "gpt-5.6-terra",
			},
			upstreamEndpoint: protocol.ResponsesEndpoint,
			converts:         true,
		},
		{
			name: "GPT public alias keeps non-GPT upstream on Chat",
			candidate: Candidate{
				PublicName:   "gpt-5.6-luna",
				UpstreamName: "qwen3-235b-a22b-instruct-2507",
			},
			upstreamEndpoint: protocol.ChatCompletionsEndpoint,
			converts:         false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := preferredProtocolPlan(protocol.ChatCompletionsEndpoint, test.candidate)
			if plan.UpstreamEndpoint() != test.upstreamEndpoint || plan.Converts() != test.converts {
				t.Fatalf("plan = endpoint %q, converts %t; want endpoint %q, converts %t", plan.UpstreamEndpoint(), plan.Converts(), test.upstreamEndpoint, test.converts)
			}
		})
	}
}
