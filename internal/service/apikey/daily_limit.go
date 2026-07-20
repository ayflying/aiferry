package apikey

import (
	"errors"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

var ErrDailySpendLimitReached = gerror.New("访问密钥今日费用已达限额")

func IsDailySpendLimitReached(err error) bool {
	return errors.Is(err, ErrDailySpendLimitReached)
}

func (key AuthKey) dailyLimitReached(now time.Time) bool {
	if key.DailySpendLimit == nil {
		return false
	}
	return dailySpentToday(key.DailySpentAmount, key.DailySpendDate, now) >= *key.DailySpendLimit
}

func dailyRemaining(limit, spent float64, spentDate *time.Time, now time.Time) float64 {
	remaining := limit - dailySpentToday(spent, spentDate, now)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func dailySpentToday(spent float64, spentDate *time.Time, now time.Time) float64 {
	if spentDate == nil || spentDate.Format(time.DateOnly) != now.Format(time.DateOnly) {
		return 0
	}
	return spent
}
