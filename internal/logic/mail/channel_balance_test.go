package mail

import "testing"

func TestLowestTriggeredChannelBalanceThreshold(t *testing.T) {
	thresholds := []float64{10, 5, 1}
	tests := []struct {
		name      string
		remaining float64
		threshold float64
		alerted   bool
	}{
		{name: "above all thresholds", remaining: 10, alerted: false},
		{name: "first threshold", remaining: 9.5, threshold: 10, alerted: true},
		{name: "middle threshold", remaining: 4.5, threshold: 5, alerted: true},
		{name: "lowest threshold", remaining: 0.500076, threshold: 1, alerted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			threshold, alerted := lowestTriggeredChannelBalanceThreshold(test.remaining, thresholds)
			if alerted != test.alerted || threshold != test.threshold {
				t.Fatalf("lowestTriggeredChannelBalanceThreshold(%v) = (%v, %t), want (%v, %t)", test.remaining, threshold, alerted, test.threshold, test.alerted)
			}
		})
	}
}
