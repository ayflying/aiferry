package system

import (
	"testing"
	"time"
)

func TestModelQualityEventViewUsesChannelName(t *testing.T) {
	view := modelQualityEventView(modelQualityEventRow{
		Id: 7, ChannelId: 3, ChannelName: "primary", CredentialId: 8, CredentialIndex: 2, APIKeyName: "宝塔",
		RequestedModel: "gpt-test", ExpectedModel: "gpt-test", ObservedModel: "gpt-test",
		ReasonsJson: `["upstream_model_tier_lower"]`, CreatedAt: time.Now(),
	})
	if view.ChannelName != "primary" {
		t.Fatalf("channel name = %q", view.ChannelName)
	}
	if view.CredentialIndex != 2 {
		t.Fatalf("credential index = %d", view.CredentialIndex)
	}
	if view.APIKeyName != "宝塔" {
		t.Fatalf("API key name = %q", view.APIKeyName)
	}
}

func TestApplyModelQualityEventReferences(t *testing.T) {
	rows := []modelQualityEventRow{
		{ChannelId: 3, CredentialId: 8, RequestId: "request-1"},
		{ChannelId: 4, CredentialId: 9, RequestId: "request-2"},
	}
	applyModelQualityEventReferences(rows, modelQualityEventReferences{
		channelNames:      map[uint64]string{3: "primary"},
		credentialIndexes: map[uint64]uint{8: 2},
		requestAPIKeyIDs:  map[string]uint64{"request-1": 7},
		apiKeyNames:       map[uint64]string{7: "codex"},
	})
	if rows[0].ChannelName != "primary" || rows[0].CredentialIndex != 2 || rows[0].APIKeyName != "codex" {
		t.Fatalf("unexpected populated row: %#v", rows[0])
	}
	if rows[1].ChannelName != "已删除渠道" || rows[1].CredentialIndex != 0 || rows[1].APIKeyName != "未记录访问密钥" {
		t.Fatalf("unexpected fallback row: %#v", rows[1])
	}
}

func TestModelQualityEventRetentionLimit(t *testing.T) {
	if maxStoredModelQualityEvents != 999 {
		t.Fatalf("retention limit = %d", maxStoredModelQualityEvents)
	}
}

func TestModelQualityRetentionCutoffWindow(t *testing.T) {
	offset, limit := modelQualityRetentionCutoffWindow()
	if offset != maxStoredModelQualityEvents || limit != 1 {
		t.Fatalf("retention cutoff window = (%d, %d), want (%d, 1)", offset, limit, maxStoredModelQualityEvents)
	}
}
