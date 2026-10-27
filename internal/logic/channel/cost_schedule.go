package channel

import (
	"context"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/schedule"
	"github.com/yunloli/aiferry/internal/model/entity"
)

var costSyncRetryDelays = []time.Duration{0, 5 * time.Minute, 10 * time.Minute}

var shanghaiLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}()

// costSyncTaskKey 是「每日余额同步」在计划任务中心的标识。
const costSyncTaskKey = "channel.cost_sync"

func (s *sChannel) StartCostSync(ctx context.Context) {
	schedule.Register(schedule.Spec{
		Key:         costSyncTaskKey,
		Name:        "每日余额同步",
		Description: "每天 00:01（北京时间）拉取各渠道上游费用与余额，失败按 5 分钟、10 分钟重试两轮；启动时先跑一次。",
		Kind:        schedule.KindDailyAt,
		Hour:        0,
		Minute:      1,
		Enabled:     true,
	})
	go func() {
		// 启动时立即同步一次：进程重启后余额不应该等到第二天零点才刷新。
		if err := schedule.Run(ctx, costSyncTaskKey, func(ctx context.Context) error {
			s.syncPlatformCosts(ctx, "startup")
			return nil
		}); err != nil {
			g.Log().Warningf(ctx, "startup cost sync failed: %v", err)
		}
		for {
			timer := time.NewTimer(nextCostSync(time.Now()))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				_ = schedule.Run(ctx, costSyncTaskKey, func(ctx context.Context) error {
					s.syncCostWindow(ctx)
					return nil
				})
			}
		}
	}()
}

func (s *sChannel) syncCostWindow(ctx context.Context) {
	for attempt, delay := range costSyncRetryDelays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		s.syncPlatformCosts(ctx, fmt.Sprintf("daily attempt %d/%d", attempt+1, len(costSyncRetryDelays)))
	}
}

func (s *sChannel) syncPlatformCosts(ctx context.Context, reason string) {
	channels := make([]entity.Channels, 0)
	if err := dao.Channels.Ctx(ctx).Scan(&channels); err != nil {
		g.Log().Warningf(ctx, "load channels for scheduled cost sync (%s): %v", reason, err)
		return
	}
	for _, channel := range channels {
		if channel.Status != 1 {
			g.Log().Debugf(ctx, "scheduled cost sync skipped disabled channel %d (%s)", channel.Id, reason)
			continue
		}
		_, config, err := s.types.GetByCode(ctx, channel.Type)
		if err != nil {
			g.Log().Warningf(ctx, "scheduled cost sync skipped channel %d (%s): load channel type: %v", channel.Id, reason, err)
			continue
		}
		if config.Costs.Adapter == channeltype.AdapterNone {
			g.Log().Debugf(ctx, "scheduled cost sync skipped channel %d without cost adapter (%s)", channel.Id, reason)
			continue
		}
		if _, err = s.QueryCost(ctx, channel.Id, adminapi.CostQueryInput{}); err != nil {
			g.Log().Warningf(ctx, "scheduled cost sync for channel %d failed (%s): %v", channel.Id, reason, err)
			continue
		}
		g.Log().Infof(ctx, "scheduled cost sync for channel %d succeeded (%s)", channel.Id, reason)
	}
}

func nextCostSync(now time.Time) time.Duration {
	now = now.In(shanghaiLocation)
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, shanghaiLocation)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(now)
}
