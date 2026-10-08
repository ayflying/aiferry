package channeltype

import (
	"path/filepath"
	"testing"

	"github.com/yunloli/aiferry/internal/config"
)

// TestValidateBuiltinsFromManifest 拿仓库里的 manifest/builtins.json 跑一遍启动期的内置
// 类型校验：生产启动在 cmd.go 中先 LoadBuiltins 再 ValidateBuiltins，任何内置渠道类型的
// quota/costs 配置写错都会让进程启动即退出（exit 1）。
//
// 内置类型此前的单测只覆盖 LoadBuiltins（JSON 能否解析），校验层没人把关，导致
// workbuddy 的 workbuddy_credits 适配器没接进 normalizeQuotaConfig 时 CI 全绿、
// 而 0.5.168 上线启动即崩。这个用例把「manifest + 校验」整条启动路径纳入测试。
func TestValidateBuiltinsFromManifest(t *testing.T) {
	registry, err := config.LoadBuiltins(filepath.Join("..", "..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatalf("LoadBuiltins: %v", err)
	}
	if len(registry.ChannelTypes) == 0 {
		t.Fatal("manifest must declare at least one built-in channel type")
	}
	if err := ValidateBuiltins(registry); err != nil {
		t.Fatalf("ValidateBuiltins must accept the shipped manifest: %v", err)
	}
}
