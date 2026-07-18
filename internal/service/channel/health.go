package channel

import (
	"context"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/system"
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
	model := dao.Channels.Ctx(ctx).As("c").
		Fields("c.id AS channel_id,c.health_check_model_id AS model_id").
		InnerJoin(dao.ChannelModels.Table()+" m", "m.id=c.health_check_model_id AND m.channel_id=c.id AND m.enabled=1 AND m.deleted_at IS NULL").
		Where("c.health_check_model_id IS NOT NULL").
		Where("c.auto_disable_enabled", 1).
		OrderAsc("c.id")
	if mode == "all" {
		model = model.Where("c.status=1 OR (c.status=0 AND c.auto_disabled_at IS NOT NULL)")
	} else {
		model = model.Where(
			"c.status=? AND c.auto_disabled_at IS NOT NULL AND c.auto_disabled_source=?",
			0,
			system.AutoDisableSourceRelayRequest,
		)
	}
	if err := model.Scan(&rows); err != nil {
		return
	}
	for _, row := range rows {
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, _ = s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: row.ModelID, Endpoint: "auto"}, usage.SystemUserID)
		cancel()
	}
	s.runCredentialHealthChecks(ctx, mode)
}

func (s *Service) runCredentialHealthChecks(ctx context.Context, mode string) {
	type healthCheckModel struct {
		ChannelID    uint64 `orm:"channel_id"`
		ModelID      uint64 `orm:"model_id"`
		CredentialID uint64 `orm:"credential_id"`
	}
	rows := make([]healthCheckModel, 0)
	model := dao.ChannelCredentials.Ctx(ctx).As("cc").
		Fields("cc.channel_id,cc.id AS credential_id,c.health_check_model_id AS model_id").
		InnerJoin(dao.Channels.Table()+" c", "c.id=cc.channel_id AND c.status=1 AND c.auto_disable_enabled=1").
		InnerJoin(dao.ChannelModels.Table()+" m", "m.id=c.health_check_model_id AND m.channel_id=c.id AND m.enabled=1 AND m.deleted_at IS NULL").
		Where("cc.status=0 AND cc.auto_disabled_at IS NOT NULL").
		OrderAsc("cc.id")
	if mode == "passive" {
		model = model.Where("cc.auto_disabled_source=?", system.AutoDisableSourceRelayRequest)
	}
	if err := model.Scan(&rows); err != nil {
		return
	}
	for _, row := range rows {
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, _ = s.TestModel(testCtx, adminapi.ModelTestInput{
			ModelID: row.ModelID, ChannelCredentialID: row.CredentialID, Endpoint: "auto",
		}, usage.SystemUserID)
		cancel()
	}
}
