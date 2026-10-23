package channel

import (
	"bytes"
	"encoding/json"
	"regexp"
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

// workBuddyZeroWidthSpace 是参考实现用来打断「精确子串」指纹的不可见字符
// （U+200B ZERO WIDTH SPACE）：渲染为空、模型照常阅读，但让上游的整段文本
// 比对不再命中。仅在上游已用 11128 拒绝后的一次性重试里使用。
const workBuddyZeroWidthSpace = "\u200b"

// workBuddyWordBreak 匹配「首字母大写的词 + 空白 + 首字母大写的词」，
// 对应参考实现的 WORD_INTERNAL_BREAK（Go 正则不支持前瞻，因此把后半词
// 一并吃进匹配再原样写回）。只拆专有名词，不碰普通行文。
var workBuddyWordBreak = regexp.MustCompile(`([A-Z][A-Za-z0-9]*)(\s+)([A-Z][A-Za-z0-9]*)`)

var (
	workBuddySentenceBreak = regexp.MustCompile(`([.!?])(\s+)`)
	workBuddyClauseBreak   = regexp.MustCompile(`([,;])(\s+)`)
	workBuddyCJKPunct      = regexp.MustCompile(`([。！？])`)
)

// PerturbWorkBuddyBody 按参考实现 perturbChannelFingerprints 的同一规则，在
// system/assistant 文本的句读边界插入零宽空格，让上游按精确子串判定的渠道
// 指纹不再命中；语义不变、对用户不可见。返回 false 表示没有可改动的内容
// （例如已扰动过），调用方不应为此多花一次上游请求。
//
// 与参考实现一致：只在 11128 拒绝之后作为最后手段调用，正常请求不加扰动；
// 幂等（已含零宽空格的文本跳过），所以重试不会叠加。
func PerturbWorkBuddyBody(body []byte) ([]byte, bool) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, false
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(payload["messages"], &messages); err != nil || len(messages) == 0 {
		return body, false
	}
	changed := false
	for i, raw := range messages {
		var message map[string]json.RawMessage
		if json.Unmarshal(raw, &message) != nil || message == nil {
			continue
		}
		var role string
		_ = json.Unmarshal(message["role"], &role)
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "system" && role != "assistant" {
			continue
		}
		content, ok := workBuddyPerturbContent(message["content"])
		if !ok {
			continue
		}
		message["content"] = content
		messages[i], _ = json.Marshal(message)
		changed = true
	}
	if !changed {
		return body, false
	}
	payload["messages"], _ = json.Marshal(messages)
	result, err := json.Marshal(payload)
	if err != nil {
		return body, false
	}
	return result, true
}

// IsWorkBuddyGateFailure 判断上游拒绝是否属于渠道指纹拦截族（code 11128）：
// 只有这一族才值得用扰动重试，其余 4xx 是确定性错误，重发没有意义。
func IsWorkBuddyGateFailure(channelType string, status int, body []byte) bool {
	if channelType != "workbuddy" || status != 400 {
		return false
	}
	return bytes.Contains(body, []byte(`"code":11128`)) || bytes.Contains(body, []byte(`"code": 11128`))
}

func workBuddyPerturbText(text string) (string, bool) {
	if text == "" || strings.Contains(text, workBuddyZeroWidthSpace) {
		return text, false
	}
	next := workBuddySentenceBreak.ReplaceAllString(text, "$1"+workBuddyZeroWidthSpace+"$2")
	next = workBuddyCJKPunct.ReplaceAllString(next, "$1"+workBuddyZeroWidthSpace)
	next = workBuddyClauseBreak.ReplaceAllString(next, "$1"+workBuddyZeroWidthSpace+"$2")
	next = workBuddyWordBreak.ReplaceAllString(next, "$1"+workBuddyZeroWidthSpace+"$2$3")
	if next == text {
		return text, false
	}
	return next, true
}

func workBuddyPerturbContent(raw json.RawMessage) (json.RawMessage, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if next, ok := workBuddyPerturbText(text); ok {
			encoded, _ := json.Marshal(next)
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
		next, ok := workBuddyPerturbText(value)
		if !ok {
			continue
		}
		record["text"], _ = json.Marshal(next)
		parts[i], _ = json.Marshal(record)
		changed = true
	}
	if !changed {
		return raw, false
	}
	encoded, _ := json.Marshal(parts)
	return encoded, true
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
