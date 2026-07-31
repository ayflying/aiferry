package relay

import (
	"testing"
	"time"
)

func TestRecordFirstStreamOutputKeepsEarliestVisibleIncrement(t *testing.T) {
	result := attemptResult{}
	recordFirstStreamOutput(&result, time.Now().Add(-20*time.Millisecond))
	if result.firstTokenMs == nil || *result.firstTokenMs < 20 {
		t.Fatalf("firstTokenMs = %v, want first visible increment timing", result.firstTokenMs)
	}
	first := *result.firstTokenMs
	recordFirstStreamOutput(&result, time.Now().Add(-time.Second))
	if *result.firstTokenMs != first {
		t.Fatalf("firstTokenMs = %d, want original %d", *result.firstTokenMs, first)
	}
}
