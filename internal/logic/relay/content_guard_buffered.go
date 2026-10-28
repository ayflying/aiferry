package relay

import (
	"encoding/json"
	"strings"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// 非流式响应的正文污染判定：与流式状态机（content_guard.go）共用形态判定与
// 思考块切分逻辑，这里只负责整包响应（chat.completion）的检查与改写。

// inspectBufferedContent 处理非流式 chat.completion 响应：先修思考块泄漏，
// 再把「正文整体就是客户端协议标记」的响应标记出来交给上层次处理。
// 第二个返回值表示该响应应当按上游失败处理，不再交付给客户端。
func inspectBufferedContent(endpoint string, body []byte) ([]byte, bool) {
	if endpoint != protocol.ChatCompletionsEndpoint || !hasAngleBracket(body) {
		return body, false
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, false
	}
	choices, ok := payload["choices"].([]any)
	if !ok {
		return body, false
	}
	changed := false
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
		switch classifyGuardPrefix(strings.TrimLeft(content, " \t\r\n")) {
		case guardMarkup:
			return body, true
		case guardThinking:
			thinking, answer, _, ok := splitLeakedThinking(strings.TrimLeft(content, " \t\r\n"))
			if !ok {
				continue
			}
			// 思考块之后的正文要重新判定：模型跑偏时是 <analysis>…</analysis>
			// 紧跟 <summary>…</summary>，那段摘要同样不能当回答交付。
			if classifyGuardPrefix(answer) == guardMarkup {
				return body, true
			}
			// 正文里混进了思考块：上游已单独给出思考时不覆盖它。
			if hasReasoning(message) {
				// 只有正文确实还带一段回答时才摘掉草稿段，避免把
				// 「只有标签、没有回答」的正文抹成空字符串。
				if answer == "" {
					continue
				}
				message["content"] = answer
				changed = true
				continue
			}
			message[reasoningFieldStandard] = thinking
			message["content"] = answer
			changed = true
		}
	}
	if !changed {
		return body, false
	}
	result, err := json.Marshal(payload)
	if err != nil {
		return body, false
	}
	return result, false
}
