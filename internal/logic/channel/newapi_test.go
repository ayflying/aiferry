package channel

import "testing"

func TestNewAPIEndpointURL(t *testing.T) {
	value, err := newAPIEndpointURL("https://newapi.example/console/v1", "/api/user/self")
	if err != nil || value != "https://newapi.example/console/api/user/self" {
		t.Fatalf("unexpected NewAPI endpoint: %q %v", value, err)
	}
	if _, err = newAPIEndpointURL("https://newapi.example", "/api/status"); err == nil {
		t.Fatal("expected base URL without /v1 to be rejected")
	}
}

func TestNewAPICostAmounts(t *testing.T) {
	used, remaining, err := newAPICostAmounts(
		[]byte(`{"success":true,"data":{"quota":1500000,"used_quota":250000}}`),
		[]byte(`{"success":true,"data":{"quota_per_unit":500000}}`),
	)
	if err != nil || used == nil || remaining == nil || *used != 0.5 || *remaining != 3 {
		t.Fatalf("unexpected NewAPI cost amounts: used=%v remaining=%v err=%v", used, remaining, err)
	}
}

func TestNewAPICostAmountsRejectsMissingQuotaUnit(t *testing.T) {
	_, _, err := newAPICostAmounts(
		[]byte(`{"success":true,"data":{"quota":100,"used_quota":50}}`),
		[]byte(`{"success":true,"data":{}}`),
	)
	if err == nil {
		t.Fatal("expected missing quota_per_unit rejection")
	}
}
