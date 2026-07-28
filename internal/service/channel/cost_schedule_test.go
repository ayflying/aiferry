package channel

import (
	"testing"
	"time"
)

func TestNextCostSync(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{
			name: "before daily sync",
			now:  time.Date(2026, time.July, 28, 0, 0, 0, 0, shanghaiLocation),
			want: time.Minute,
		},
		{
			name: "at daily sync",
			now:  time.Date(2026, time.July, 28, 0, 1, 0, 0, shanghaiLocation),
			want: 24 * time.Hour,
		},
		{
			name: "during the day",
			now:  time.Date(2026, time.July, 28, 11, 9, 16, 0, shanghaiLocation),
			want: 12*time.Hour + 52*time.Minute - 16*time.Second,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextCostSync(test.now); got != test.want {
				t.Fatalf("nextCostSync() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestCostSyncRetryDelays(t *testing.T) {
	want := []time.Duration{0, 5 * time.Minute, 10 * time.Minute}
	if len(costSyncRetryDelays) != len(want) {
		t.Fatalf("retry delay count = %d, want %d", len(costSyncRetryDelays), len(want))
	}
	for index, value := range want {
		if costSyncRetryDelays[index] != value {
			t.Fatalf("retry delay %d = %s, want %s", index, costSyncRetryDelays[index], value)
		}
	}
}
