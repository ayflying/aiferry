package channel

import (
	"testing"
	"time"
)

func TestCostRangeDefaultsToCurrentMonth(t *testing.T) {
	start, end, err := costRange("", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if start.Day() != 1 || start.Month() != now.Month() || !end.After(start) {
		t.Fatalf("unexpected range: %v - %v", start, end)
	}
}

func TestJSONFloatPaths(t *testing.T) {
	body := []byte(`{"usage":{"cost":"12.34"},"remaining":8.5}`)
	used := jsonFloat(body, "usage.cost")
	remaining := firstFloat(body, "missing", "remaining")
	if used == nil || *used != 12.34 || remaining == nil || *remaining != 8.5 {
		t.Fatalf("unexpected values: %v %v", used, remaining)
	}
}

func TestResolveCostURL(t *testing.T) {
	value, err := resolveCostURL("https://relay.example/v1", "usage")
	if err != nil || value != "https://relay.example/v1/usage" {
		t.Fatalf("unexpected URL: %q %v", value, err)
	}
}
