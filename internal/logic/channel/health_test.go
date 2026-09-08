package channel

import (
	"testing"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
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
