package relay

import (
	"encoding/json"
	"strings"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// 非流式响应的协议标记判定：与流式状态机（content_guard.go）共用同一套开头形态
// 判定，这里只负责整包响应（chat.completion）的检查。
//
// 2026-10-29：原先这里还会把正文里的思考块搬进 reasoning_content，那套改写随后者
// 一起删除（详见 content_guard.go 头部说明）。现在非流式响应只在「正文整体以协议
// 标记开头」时判定为上游坏响应，其余一律原样返回，不改写任何字段。

// inspectBufferedMarkup 判断非流式 chat.completion 响应是否把客户端内部标记写进了
// 正文。返回 true 表示该响应应当按上游失败处理，不再交付给客户端。
func inspectBufferedMarkup(endpoint string, body []byte) bool {
	if endpoint != protocol.ChatCompletionsEndpoint || !hasAngleBracket(body) {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	choices, ok := payload["choices"].([]any)
	if !ok {
		return false
	}
	for _, value := range choices {
		choice, ok := value.(map[string]any)
		if !ok {
			continue
		}
		message, ok := choice["message"].(map[string]any)
		if !ok {
			continue
		}
		content, ok := message["content"].(string)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		if classifyGuardPrefix(strings.TrimLeft(content, " \t\r\n")) == guardMarkup {
			return true
		}
	}
	return false
}
