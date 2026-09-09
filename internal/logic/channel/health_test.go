package channel

import (
	"testing"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/system"
)

func TestHealthCheckDueUsesConfiguredInterval(t *testing.T) {
	last := time.Date(2026, time.July, 22, 17, 47, 8, 0, time.UTC)
	interval := 10 * time.Minute
	if healthCheckDue(last.Add(9*time.Minute+59*time.Second), last, interval) {
		t.Fatal("health check ran before the configured interval")
	}
	if !healthCheckDue(last.Add(interval), last, interval) {
		t.Fatal("health check did not run at the configured interval")
	}
	if healthCheckDue(last.Add(interval), last, 0) {
		t.Fatal("zero interval must not schedule a health check")
	}
}

// 回归：恢复检查不得被健康检查开关拖住。默认配置（探测关闭、恢复开启）
// 下被自动禁用的模型必须仍会得到自动测试解禁——智谱渠道扣分禁用后
// 永不解禁的根因就是恢复检查挂在探测开关下。
func TestHealthActionsDecoupleRecoveryFromProbe(t *testing.T) {
	if recovery, regular := healthActions(adminapi.SystemResilienceSettingsInput{RecoveryEnabled: true, HealthCheckEnabled: false}); !recovery || regular {
		t.Fatalf("recovery-only settings: recovery=%v regular=%v", recovery, regular)
	}
	if recovery, regular := healthActions(adminapi.SystemResilienceSettingsInput{RecoveryEnabled: true, HealthCheckEnabled: true}); !recovery || !regular {
		t.Fatalf("both enabled: recovery=%v regular=%v", recovery, regular)
	}
	if recovery, _ := healthActions(adminapi.SystemResilienceSettingsInput{RecoveryEnabled: false, HealthCheckEnabled: true}); recovery {
		t.Fatal("recovery disabled must not schedule recovery checks")
	}
}

// 回归：被动模式下模型/密钥恢复巡检不得按禁用来源过滤。model_test 来源
// 禁用的模型若被来源过滤排除，将永远无人重测、永不解禁——生产实测渠道 9
// 两个被测试失败禁用的模型在被动模式下从未被巡检过。
func TestRecoverySourceRestriction(t *testing.T) {
	// 被动模式：渠道恢复仍按来源过滤（model_test 来源的渠道关闭由模型恢复间接解禁）
	if source, restrict := recoverySourceRestriction("passive", system.RecoveryTargetChannel); !restrict || source != system.AutoDisableSourceRelayRequest {
		t.Fatalf("passive channel recovery should restrict to relay_request, got restrict=%v source=%q", restrict, source)
	}
	// 被动模式：模型与密钥恢复不过滤来源
	if _, restrict := recoverySourceRestriction("passive", system.RecoveryTargetModel); restrict {
		t.Fatal("passive model recovery must not filter by disable source")
	}
	if _, restrict := recoverySourceRestriction("passive", system.RecoveryTargetCredential); restrict {
		t.Fatal("passive credential recovery must not filter by disable source")
	}
	// 主动模式：全都不限制
	for _, target := range []system.RecoveryTarget{system.RecoveryTargetChannel, system.RecoveryTargetCredential, system.RecoveryTargetModel} {
		if _, restrict := recoverySourceRestriction("all", target); restrict {
			t.Fatalf("all mode must not filter recovery for target %s", target)
		}
	}
}
