package channel

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/usage"
)

const healthCheckTick = 10 * time.Second

func (s *Service) StartHealthChecks(ctx context.Context) {
	go func() {
		lastHealthCheck := time.Now()
		ticker := time.NewTicker(healthCheckTick)
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
				if !healthCheckDue(now, lastHealthCheck, interval) {
					continue
				}
				lastHealthCheck = now
				s.runRecoveryChecks(ctx, settings.HealthCheckMode)
				s.runRegularHealthChecks(ctx, settings.HealthCheckMode)
			}
		}
	}()
}

func healthCheckDue(now, last time.Time, interval time.Duration) bool {
	return interval > 0 && !now.Before(last.Add(interval))
}

func (s *Service) runRegularHealthChecks(ctx context.Context, mode string) {
	if mode != "all" {
		return
	}
	modelID := healthCheckModelIDExpression("c")
	type healthCheckModel struct {
		ModelID uint64 `orm:"model_id"`
	}
	rows := make([]healthCheckModel, 0)
	if err := dao.Channels.Ctx(ctx).As("c").
		Fields(modelID+" AS model_id").
		InnerJoin(dao.ChannelModels.Table()+" m", healthCheckModelJoin("c", "m")).
		Where("c.status=1").
		Where("c.auto_disable_enabled", 1).
		OrderAsc("c.id").
		Scan(&rows); err != nil {
		g.Log().Warningf(ctx, "load regular channel health checks: %v", err)
		return
	}
	for _, row := range rows {
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, _ = s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: row.ModelID, Endpoint: "auto"}, usage.SystemUserID)
		cancel()
	}
}

func (s *Service) runRecoveryChecks(ctx context.Context, mode string) {
	s.runChannelRecoveryChecks(ctx, mode)
	s.runCredentialRecoveryChecks(ctx, mode)
}

func (s *Service) runChannelRecoveryChecks(ctx context.Context, mode string) {
	type healthCheckModel struct {
		ChannelID      uint64    `orm:"channel_id"`
		ModelID        uint64    `orm:"model_id"`
		AutoDisabledAt time.Time `orm:"auto_disabled_at"`
	}
	rows := make([]healthCheckModel, 0)
	modelID := healthCheckModelIDExpression("c")
	model := dao.Channels.Ctx(ctx).As("c").
		Fields("c.id AS channel_id,"+modelID+" AS model_id,c.auto_disabled_at").
		InnerJoin(dao.ChannelModels.Table()+" m", healthCheckModelJoin("c", "m")).
		Where("c.status=0 AND c.auto_disabled_at IS NOT NULL").
		Where("c.auto_disable_enabled", 1).
		OrderAsc("c.id")
	if mode == "passive" {
		model = model.Where("c.auto_disabled_source=?", system.AutoDisableSourceRelayRequest)
	}
	if err := model.Scan(&rows); err != nil {
		g.Log().Warningf(ctx, "load channel recovery checks: %v", err)
		return
	}
	for _, row := range rows {
		started, err := s.resilience.BeginRecoveryAttempt(ctx, system.RecoveryTargetChannel, row.ChannelID, row.AutoDisabledAt)
		if err != nil {
			g.Log().Warningf(ctx, "schedule channel %d recovery: %v", row.ChannelID, err)
			continue
		}
		if !started {
			continue
		}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		result, testErr := s.TestModel(testCtx, adminapi.ModelTestInput{ModelID: row.ModelID, Endpoint: "auto"}, usage.SystemUserID)
		cancel()
		s.resilience.FinishRecoveryAttempt(ctx, system.RecoveryTargetChannel, row.ChannelID, testErr == nil && result.Success)
	}
}

func (s *Service) runCredentialRecoveryChecks(ctx context.Context, mode string) {
	type healthCheckModel struct {
		ChannelID      uint64    `orm:"channel_id"`
		ModelID        uint64    `orm:"model_id"`
		CredentialID   uint64    `orm:"credential_id"`
		AutoDisabledAt time.Time `orm:"auto_disabled_at"`
	}
	rows := make([]healthCheckModel, 0)
	modelID := healthCheckModelIDExpression("c")
	model := dao.ChannelCredentials.Ctx(ctx).As("cc").
		Fields("cc.channel_id,cc.id AS credential_id,"+modelID+" AS model_id,cc.auto_disabled_at").
		InnerJoin(dao.Channels.Table()+" c", "c.id=cc.channel_id AND c.status=1 AND c.auto_disable_enabled=1").
		InnerJoin(dao.ChannelModels.Table()+" m", healthCheckModelJoin("c", "m")).
		Where("cc.status=0 AND cc.auto_disabled_at IS NOT NULL").
		OrderAsc("cc.id")
	if mode == "passive" {
		model = model.Where("cc.auto_disabled_source=?", system.AutoDisableSourceRelayRequest)
	}
	if err := model.Scan(&rows); err != nil {
		g.Log().Warningf(ctx, "load credential recovery checks: %v", err)
		return
	}
	for _, row := range rows {
		started, err := s.resilience.BeginRecoveryAttempt(ctx, system.RecoveryTargetCredential, row.CredentialID, row.AutoDisabledAt)
		if err != nil {
			g.Log().Warningf(ctx, "schedule credential %d recovery: %v", row.CredentialID, err)
			continue
		}
		if !started {
			continue
		}
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		result, testErr := s.TestModel(testCtx, adminapi.ModelTestInput{
			ModelID: row.ModelID, ChannelCredentialID: row.CredentialID, Endpoint: "auto",
		}, usage.SystemUserID)
		cancel()
		s.resilience.FinishRecoveryAttempt(ctx, system.RecoveryTargetCredential, row.CredentialID, testErr == nil && result.Success)
	}
}
