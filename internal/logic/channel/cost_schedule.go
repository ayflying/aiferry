package channel

import (
	"context"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
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

func (s *sChannel) StartCostSync(ctx context.Context) {
	go func() {
		s.syncPlatformCosts(ctx, "startup")
		for {
			timer := time.NewTimer(nextCostSync(time.Now()))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.syncCostWindow(ctx)
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
