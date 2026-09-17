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
		MessagesPath:   "https://global.example/v1/messages",
	}
	// 类型级名单优先于全局名单：同一模型两处都命中时用类型专属端点。
	if url := ResolveMessagesEndpoint(typeConfig, settings, "claude-sonnet-x"); url != "https://type.example/v1/messages" {
		t.Fatalf("type declaration should win, got %q", url)
	}
	// 类型级未命中的模型退回全局名单。
	if url := ResolveMessagesEndpoint(typeConfig, settings, "union-alpha"); url != "https://global.example/v1/messages" {
		t.Fatalf("global list should be used as fallback, got %q", url)
	}
}

func TestResolveMessagesEndpointGlobalOnly(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{
		MessagesModels: []string{"union-*", "qwen3.7-max"},
		MessagesPath:   "",
	}
	// 渠道类型读取失败（空配置）时全局名单仍然生效；端点留空回退协议缺省值。
	if url := ResolveMessagesEndpoint(Config{}, settings, "union-beta"); url != protocol.MessagesEndpoint {
		t.Fatalf("empty path should fall back to default endpoint, got %q", url)
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
