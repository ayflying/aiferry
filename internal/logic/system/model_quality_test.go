package system

import (
	"testing"
	"time"
)

func TestModelQualityEventViewUsesChannelName(t *testing.T) {
	view := modelQualityEventView(modelQualityEventRow{
		Id: 7, ChannelId: 3, ChannelName: "primary", CredentialId: 8,
		RequestedModel: "gpt-test", ExpectedModel: "gpt-test", ObservedModel: "gpt-test",
		ReasonsJson: `["upstream_model_tier_lower"]`, CreatedAt: time.Now(),
	})
	if view.ChannelName != "primary" {
		t.Fatalf("channel name = %q", view.ChannelName)
	}
}
