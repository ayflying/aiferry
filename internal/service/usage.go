// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
	. "github.com/yunloli/aiferry/internal/logic/usage"
	"time"
)

type (
	IUsage interface {
		ParseDashboardRange(ctx context.Context, startValue string, endValue string, days int, hours int) (DashboardRange, error)
		ParseLogRange(ctx context.Context, startValue string, endValue string) (time.Time, time.Time, error)
		Dashboard(ctx context.Context, dateRange DashboardRange) (Dashboard, error)
		UserSummary(ctx context.Context, userID uint64, days int) (UserSummary, error)
		List(ctx context.Context, input LogFilter) (LogPage, error)
		Record(ctx context.Context, input RecordInput) error
	}
)

var (
	localUsage IUsage
)

func Usage() IUsage {
	if localUsage == nil {
		panic("implement not found for interface IUsage, forgot register?")
	}
	return localUsage
}

func RegisterUsage(i IUsage) {
	localUsage = i
}
