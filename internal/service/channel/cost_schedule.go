package channel

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

var shanghaiLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return location
}()

func (s *Service) StartCostSync(ctx context.Context) {
	go func() {
		for {
			timer := time.NewTimer(nextCostSync(time.Now()))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.syncPlatformCosts(ctx)
			}
		}
	}()
}

func (s *Service) syncPlatformCosts(ctx context.Context) {
	channels := make([]entity.Channels, 0)
	if err := dao.Channels.Ctx(ctx).Scan(&channels); err != nil {
		g.Log().Warningf(ctx, "load channels for scheduled cost sync: %v", err)
		return
	}
	for _, channel := range channels {
		if channel.Status != 1 {
			continue
		}
		_, config, err := s.types.GetByCode(ctx, channel.Type)
		if err != nil || config.Costs.Adapter == channeltype.AdapterNone {
			continue
		}
		if _, err = s.QueryCost(ctx, channel.Id, adminapi.CostQueryInput{}); err != nil {
			g.Log().Warningf(ctx, "scheduled cost sync for channel %d failed: %v", channel.Id, err)
			continue
		}
		if s.mail != nil {
			if err = s.mail.ClearChannelLowBalanceAlerts(ctx, channel.Id); err != nil {
				g.Log().Warningf(ctx, "clear channel %d low balance mail alerts: %v", channel.Id, err)
			}
		}
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
