package channeltype

import (
	"strings"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// MatchesMessagesModel 判断上游模型名是否命中渠道类型声明的 Messages 模型
// 名单。名单项两种写法：`前缀-*` 通配（如 union-*）与精确名（如
// qwen3.7-max）。名单为空视为未声明，绝不按空前缀全量命中。
func MatchesMessagesModel(config Config, model string) bool {
	return matchMessagePatterns(config.Protocol.MessagesModels, model)
}

// MatchesRerankModel reports whether a model is declared for the standard rerank API.
func MatchesRerankModel(config Config, model string) bool {
	if matchMessagePatterns(config.Protocol.RerankModels, model) {
		return true
	}
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "rerank")
}

// ResolveMessagesEndpoint 判定该上游模型应使用的 Messages 端点：渠道类型
// 声明的名单优先（类型专属知识），类型级未配置时退回全局名单（系统设置）。
// 命中返回端点 URL（完整 URL 原样、相对路径由调用方拼接、空回退协议缺省值），
// 未命中返回空串。
//
// 端点地址只由**渠道类型**声明：Messages 端点是渠道专属事实（各家用各自的域名与
// 路径前缀），全局设置里放一个固定地址会让所有命中名单的渠道都被送到同一个域名。
// 全局名单只承载「哪些模型名说 Anthropic 协议」这类与渠道无关的模型知识，命中后
// 使用协议缺省的 /v1/messages 相对路径，用户要非标准路径就在渠道类型里声明。
func ResolveMessagesEndpoint(typeConfig Config, settings adminapi.SystemResilienceSettingsInput, model string) string {
	if matchMessagePatterns(typeConfig.Protocol.MessagesModels, model) {
		return MessagesEndpointURL(typeConfig)
	}
	if len(settings.MessagesModels) > 0 && matchMessagePatterns(settings.MessagesModels, model) {
		return protocol.MessagesEndpoint
	}
	return ""
}

// matchMessagePatterns 是名单匹配的通用实现，渠道类型名单与全局名单共用。
func matchMessagePatterns(patterns []string, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
			if strings.HasPrefix(model, prefix) {
				return true
			}
			continue
		}
		if model == pattern {
			return true
		}
	}
	return false
}

// MessagesEndpointURL 解析渠道类型声明的 Messages 端点地址：完整 URL 原样
// 返回（由调用方跳过根地址拼接），相对路径原样返回，留空回退协议缺省值。
func MessagesEndpointURL(config Config) string {
	if path := strings.TrimSpace(config.Protocol.MessagesPath); path != "" {
		return path
	}
	return protocol.MessagesEndpoint
}
