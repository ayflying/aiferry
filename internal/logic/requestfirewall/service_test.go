package requestfirewall

import (
	"context"
	"testing"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestFirewallLimitsConcurrentRequestsAndReleasesSlots(t *testing.T) {
	service := New(nil)
	service.SetSettings(testSettings())
	first, rejection := service.acquireAt(RequestInput{ClientIP: "203.0.113.10", APIKeyID: 1}, time.Now())
	if rejection != nil {
		t.Fatalf("first request rejected: %+v", rejection)
	}
	if _, rejection = service.acquireAt(RequestInput{ClientIP: "203.0.113.11", APIKeyID: 2}, time.Now()); rejection == nil {
		t.Fatal("second concurrent request must be rejected by global limit")
	}
	first()
	if _, rejection = service.acquireAt(RequestInput{ClientIP: "203.0.113.11", APIKeyID: 2}, time.Now()); rejection != nil {
		t.Fatalf("released slot must be reusable: %+v", rejection)
	}
}

func TestFirewallLimitsRatePerAPIKeyWithoutWritingPersistentState(t *testing.T) {
	service := New(nil)
	settings := testSettings()
	settings.MaxConcurrentRequests = 10
	settings.MaxConcurrentRequestsPerIP = 10
	settings.MaxConcurrentRequestsPerKey = 10
	settings.RequestsPerMinutePerIP = 2
	settings.RequestsPerMinutePerAPIKey = 2
	service.SetSettings(settings)
	now := time.Now()
	for range 2 {
		release, rejection := service.acquireAt(RequestInput{ClientIP: "203.0.113.10", APIKeyID: 1}, now)
		if rejection != nil {
			t.Fatalf("allowed request rejected: %+v", rejection)
		}
		release()
	}
	if _, rejection := service.acquireAt(RequestInput{ClientIP: "203.0.113.11", APIKeyID: 1}, now); rejection == nil || rejection.RetryAfter < 1 {
		t.Fatalf("API key rate limit was not enforced: %+v", rejection)
	}
	if release, rejection := service.Acquire(context.Background(), RequestInput{ClientIP: "203.0.113.11", APIKeyID: 2}); rejection != nil {
		t.Fatalf("different API key should have an independent bucket: %+v", rejection)
	} else {
		release()
	}
}

func TestFirewallLimitsUnauthenticatedRequestsByClient(t *testing.T) {
	service := New(nil)
	settings := testSettings()
	settings.MaxConcurrentRequests = 1
	settings.MaxConcurrentRequestsPerIP = 1
	service.SetSettings(settings)
	now := time.Now()
	release, rejection := service.acquireClientAt("203.0.113.10", now)
	if rejection != nil {
		t.Fatalf("first client request rejected: %+v", rejection)
	}
	if _, rejection = service.acquireClientAt("203.0.113.11", now); rejection == nil {
		t.Fatal("second unauthenticated request must be rejected by the global limit")
	}
	release()
}

func TestFirewallKeyAdmissionDoesNotDuplicateClientLimits(t *testing.T) {
	service := New(nil)
	service.SetSettings(testSettings())
	now := time.Now()
	clientRelease, rejection := service.acquireClientAt("203.0.113.10", now)
	if rejection != nil {
		t.Fatalf("client admission rejected: %+v", rejection)
	}
	apiKeyRelease, rejection := service.acquireAPIKeyAt(1, now)
	if rejection != nil {
		t.Fatalf("API key admission must not consume global or IP slots twice: %+v", rejection)
	}
	apiKeyRelease()
	clientRelease()
}

func TestFirewallCanBeDisabled(t *testing.T) {
	service := New(nil)
	settings := testSettings()
	settings.Enabled = false
	service.SetSettings(settings)
	for range 3 {
		if release, rejection := service.Acquire(context.Background(), RequestInput{ClientIP: "203.0.113.10", APIKeyID: 1}); rejection != nil {
			t.Fatalf("disabled firewall rejected request: %+v", rejection)
		} else {
			release()
		}
	}
}

func testSettings() adminapi.RequestFirewallSettingsInput {
	return adminapi.RequestFirewallSettingsInput{
		Enabled: true, MaxConcurrentRequests: 1, MaxConcurrentRequestsPerIP: 1, MaxConcurrentRequestsPerKey: 1,
		RequestsPerMinutePerIP: 10, RequestsPerMinutePerAPIKey: 10,
	}
}
