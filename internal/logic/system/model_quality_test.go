package system

import (
	"testing"
	"time"
)

func TestModelQualityEventViewUsesChannelName(t *testing.T) {
	view := modelQualityEventView(modelQualityEventRow{
		Id: 7, ChannelId: 3, ChannelName: "primary", CredentialId: 8, CredentialIndex: 2,
		RequestedModel: "gpt-test", ExpectedModel: "gpt-test", ObservedModel: "gpt-test",
		ReasonsJson: `["upstream_model_tier_lower"]`, CreatedAt: time.Now(),
	})
	if view.ChannelName != "primary" {
		t.Fatalf("channel name = %q", view.ChannelName)
	}
	if view.CredentialIndex != 2 {
		t.Fatalf("credential index = %d", view.CredentialIndex)
	}
}

func TestModelQualityEventRetentionLimit(t *testing.T) {
	if maxStoredModelQualityEvents != 999 {
		t.Fatalf("retention limit = %d", maxStoredModelQualityEvents)
	}
}
