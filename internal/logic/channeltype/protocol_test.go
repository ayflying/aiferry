package channeltype

import (
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/protocol"
)

func TestResolveMessagesEndpointPrefersTypeDeclaration(t *testing.T) {
	typeConfig := Config{}
	typeConfig.Protocol.MessagesModels = []string{"claude-*"}
	typeConfig.Protocol.MessagesPath = "https://type.example/v1/messages"
	settings := adminapi.SystemResilienceSettingsInput{
		MessagesModels: []string{"union-*"},
	}
	// 类型级名单优先于全局名单：同一模型两处都命中时用类型专属端点。
	if url := ResolveMessagesEndpoint(typeConfig, settings, "claude-sonnet-x"); url != "https://type.example/v1/messages" {
		t.Fatalf("type declaration should win, got %q", url)
	}
	// 类型级未命中的模型退回全局名单：端点不再来自全局设置，一律用协议缺省值。
	if url := ResolveMessagesEndpoint(typeConfig, settings, "union-alpha"); url != protocol.MessagesEndpoint {
		t.Fatalf("global list should fall back to the default endpoint, got %q", url)
	}
}

// Messages 端点是渠道专属事实：全局设置只有名单、没有地址，命中全局名单的模型
// 不能被打到某个特定渠道的域名上。
func TestResolveMessagesEndpointGlobalHasNoAddress(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{
		MessagesModels: []string{"union-*", "qwen3.7-max"},
	}
	if url := ResolveMessagesEndpoint(Config{}, settings, "union-beta"); url != protocol.MessagesEndpoint {
		t.Fatalf("global hit must use the default relative path, got %q", url)
	}
	if url := ResolveMessagesEndpoint(Config{}, settings, "qwen3.7-max"); url != protocol.MessagesEndpoint {
		t.Fatalf("exact name should match, got %q", url)
	}
	// 未命中名单返回空串，调用方继续走模型名推断。
	if url := ResolveMessagesEndpoint(Config{}, settings, "deepseek-flash"); url != "" {
		t.Fatalf("unmatched model should return empty, got %q", url)
	}
	// 全局名单为空时不启用名单转换。
	if url := ResolveMessagesEndpoint(Config{}, adminapi.SystemResilienceSettingsInput{}, "union-alpha"); url != "" {
		t.Fatalf("empty global list should be disabled, got %q", url)
	}
}

// 渠道类型声明了名单但没写路径时，用自己的缺省 /v1/messages 拼在该渠道 baseUrl 后。
func TestResolveMessagesEndpointTypeDeclarationUsesOwnDefault(t *testing.T) {
	typeConfig := Config{}
	typeConfig.Protocol.MessagesModels = []string{"union-*"}
	if url := ResolveMessagesEndpoint(typeConfig, adminapi.SystemResilienceSettingsInput{}, "union-alpha"); url != protocol.MessagesEndpoint {
		t.Fatalf("type declaration without path should use the default, got %q", url)
	}
}
