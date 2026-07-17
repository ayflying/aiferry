package channel

import (
	"context"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/usage"
)

func (s *Service) StartHealthChecks(ctx context.Context) {
	go func() {
		var lastCheck time.Time
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				settings, err := s.resilience.Get(ctx)
				if err != nil || !settings.HealthCheckEnabled || !settings.RecoveryEnabled {
					continue
				}
				interval := time.Duration(settings.HealthCheckIntervalMinutes) * time.Minute
				if !lastCheck.IsZero() && now.Sub(lastCheck) < interval {
					continue
				}
				lastCheck = now
				s.runHealthChecks(ctx, settings.HealthCheckMode)
			}
		}
	}()
}

func (s *Service) runHealthChecks(ctx context.Context, mode string) {
	type healthCheckModel struct {
		ChannelID uint64 `orm:"channel_id"`
		ModelID   uint64 `orm:"model_id"`
	}
	rows := make([]healthCheckModel, 0)
	model := dao.ChannelModels.Ctx(ctx).As("m").
		Fields("m.channel_id,m.id AS model_id").
		InnerJoin(dao.Channels.Table()+" c", "c.id=m.channel_id").
		Where("m.enabled", 1).
		OrderAsc("m.channel_id").
		OrderAsc("m.id")
	if mode == "all" {
		model = model.Where("c.status=1 OR (c.status=0 AND c.auto_disabled_at IS NOT NULL)")
	} else {
		model = model.Where("c.status=0 AND c.auto_disabled_at IS NOT NULL")
	}
	if err := model.Scan(&rows); err != nil {
		return
	}
	seen := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.ChannelID]; exists {
			continue
		}
		seen[row.ChannelID] = struct{}{}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, _ = s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: row.ModelID, Endpoint: "chat"}, usage.SystemUserID)
		cancel()
	}
}
