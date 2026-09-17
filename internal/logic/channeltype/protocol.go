package channeltype

import (
	"strings"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// MatchesMessagesModel 判断上游模型名是否命中渠道类型声明的 Messages 模型
// 名单。名单项两种写法：`前缀-*` 通配（如 union-*）与精确名（如
// qwen3.7-max）。名单为空视为未声明，绝不按空前缀全量命中。
func MatchesMessagesModel(config Config, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || len(config.Protocol.MessagesModels) == 0 {
		return false
	}
	for _, pattern := range config.Protocol.MessagesModels {
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
