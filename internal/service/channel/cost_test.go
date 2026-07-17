package channel

import (
	"testing"
	"time"
)

func TestCostRangeDefaultsToCurrentDay(t *testing.T) {
	start, end, err := costRange("", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if start.Year() != now.Year() || start.Month() != now.Month() || start.Day() != now.Day() ||
		start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || !end.After(start) {
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

func TestResolveEndpointURL(t *testing.T) {
	value, err := resolveEndpointURL("https://relay.example/v1", "usage")
	if err != nil || value != "https://relay.example/v1/usage" {
		t.Fatalf("unexpected URL: %q %v", value, err)
	}
	value, err = resolveEndpointURL("https://relay.example/v1", "/models")
	if err != nil || value != "https://relay.example/v1/models" {
		t.Fatalf("unexpected leading-slash URL: %q %v", value, err)
	}
}
