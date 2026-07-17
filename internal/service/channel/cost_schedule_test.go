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
			name: "one minute after midnight",
			now:  time.Date(2026, time.July, 17, 0, 0, 30, 0, shanghaiLocation),
			want: 30 * time.Second,
		},
		{
			name: "after scheduled time uses next day",
			now:  time.Date(2026, time.July, 17, 0, 1, 0, 0, shanghaiLocation),
			want: 24 * time.Hour,
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
