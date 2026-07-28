package apikey

import (
	"testing"
	"time"
)

func TestAuthKeyDailyLimitReached(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.Local)
	limit := 3.0
	today := now
	key := AuthKey{DailySpendLimit: &limit, DailySpentAmount: 2.99, DailySpendDate: &today}
	if key.dailyLimitReached(now) {
		t.Fatal("daily limit should allow spend below the limit")
	}
	key.DailySpentAmount = 3
	if !key.dailyLimitReached(now) {
		t.Fatal("daily limit should reject spend at the limit")
	}
	yesterday := now.AddDate(0, 0, -1)
	key.DailySpendDate = &yesterday
	if key.dailyLimitReached(now) {
		t.Fatal("a prior day amount must not consume today's limit")
	}
}

func TestDailyRemaining(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.Local)
	today := now
	if got := dailyRemaining(5, 1.25, &today, now); got != 3.75 {
		t.Fatalf("daily remaining = %v, want 3.75", got)
	}
	yesterday := now.AddDate(0, 0, -1)
	if got := dailyRemaining(5, 1.25, &yesterday, now); got != 5 {
		t.Fatalf("daily remaining after reset = %v, want 5", got)
	}
}
