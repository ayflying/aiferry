package channel

import (
	"testing"
	"time"
)

func TestHealthCheckDueUsesConfiguredInterval(t *testing.T) {
	last := time.Date(2026, time.July, 22, 17, 47, 8, 0, time.UTC)
	interval := 10 * time.Minute
	if healthCheckDue(last.Add(9*time.Minute+59*time.Second), last, interval) {
		t.Fatal("health check ran before the configured interval")
	}
	if !healthCheckDue(last.Add(interval), last, interval) {
		t.Fatal("health check did not run at the configured interval")
	}
	if healthCheckDue(last.Add(interval), last, 0) {
		t.Fatal("zero interval must not schedule a health check")
	}
}
