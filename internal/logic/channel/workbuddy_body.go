package channel

import (
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// WorkBuddy 请求兼容逻辑移植自 dsh-connect-workbuddy（MIT，Copyright 2026 LaoDing）。
// 固定参考提交：4cdba8a37b277a465b754328737f580254552d34。
// src/upstream.ts: ensureSystemHead / neutralizeChannelFingerprints。
// 来源、范围和更新步骤见 docs/workbuddy-compatibility.md；许可证见 THIRD_PARTY_NOTICES.md。
const workBuddyFallbackSystemPrompt = "You are a helpful assistant."

var workBuddyChannelOpenings = []string{
	"you are an ai agent powered by deepseek harness",
	"you are claude code, anthropic's official cli for claude",
}

// ApplyWorkBuddyBody 保留原内容，仅对参考实现识别的 system/assistant 开头
// 添加中性提示行；developer 规范化为 system，缺少 system 首消息时补齐。
// 非 WorkBuddy 渠道由调用方隔离；流式转换仍由已有 relay 管理。
func ApplyWorkBuddyBody(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, gerror.Wrap(err, "decode workbuddy request")
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(payload["messages"], &messages); err != nil || len(messages) == 0 {
		return body, nil
	}
	changed := false
	for i, raw := range messages {
		var message map[string]json.RawMessage
		if json.Unmarshal(raw, &message) != nil || message == nil {
			continue
		}
		var role string
		_ = json.Unmarshal(message["role"], &role)
		messageChanged := false
		if role == "developer" {
			role = "system"
			message["role"] = json.RawMessage(`"system"`)
			messageChanged = true
		}
		normalizedRole := strings.ToLower(strings.TrimSpace(role))
		if normalizedRole == "system" || normalizedRole == "assistant" {
			if content, ok := workBuddyNeutralContent(message["content"]); ok {
				message["content"] = content
				messageChanged = true
			}
		}
		if messageChanged {
			messages[i], _ = json.Marshal(message)
			changed = true
		}
	}
	var head map[string]json.RawMessage
	if json.Unmarshal(messages[0], &head) == nil && head != nil {
		var role string
		_ = json.Unmarshal(head["role"], &role)
		if !strings.EqualFold(strings.TrimSpace(role), "system") {
			headRaw, _ := json.Marshal(map[string]string{"role": "system", "content": workBuddyFallbackSystemPrompt})
			messages = append([]json.RawMessage{headRaw}, messages...)
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	payload["messages"], _ = json.Marshal(messages)
	result, err := json.Marshal(payload)
	if err != nil {
		return nil, gerror.Wrap(err, "encode workbuddy request")
	}
	return result, nil
}

func workBuddyNeutralText(text string) (string, bool) {
	opening := strings.ToLower(strings.TrimSpace(text))
	for _, prefix := range workBuddyChannelOpenings {
		if strings.HasPrefix(opening, prefix) {
			return workBuddyFallbackSystemPrompt + "\n\n" + text, true
		}
	}
	return text, false
}

func workBuddyNeutralContent(raw json.RawMessage) (json.RawMessage, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if result, ok := workBuddyNeutralText(text); ok {
			encoded, _ := json.Marshal(result)
			return encoded, true
		}
		return raw, false
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return raw, false
	}
	changed := false
	for i, part := range parts {
		var record map[string]json.RawMessage
		if json.Unmarshal(part, &record) != nil || record == nil {
			continue
		}
		var kind, value string
		_ = json.Unmarshal(record["type"], &kind)
		if !strings.EqualFold(strings.TrimSpace(kind), "text") || json.Unmarshal(record["text"], &value) != nil {
			continue
		}
		if result, ok := workBuddyNeutralText(value); ok {
			record["text"], _ = json.Marshal(result)
			parts[i], _ = json.Marshal(record)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	encoded, _ := json.Marshal(parts)
	return encoded, true
}
