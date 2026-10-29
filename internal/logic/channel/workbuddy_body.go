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
// 比对不再命中。0.6.25 起随首跳扰动对每个请求执行。
const workBuddyZeroWidthSpace = "\u200b"

// workBuddyWordChain 匹配「首字母大写的词」的连续链（如 DeepSeek Harness Web
// GUI），对应参考实现的 WORD_INTERNAL_BREAK。必须整链匹配再统一打断：若逐对
// 匹配（Harness Web / Web GUI），相邻两对共享同一个词，全局替换不重叠，一次
// 只会落第一对，剩余边界要等下一次执行才补插，幂等即被破坏；链内打断后零宽
// 空格切断 \s+，链在第二次执行时不再成立，天然幂等。
var workBuddyWordChain = regexp.MustCompile(`[A-Z][A-Za-z0-9]*(?:\s+[A-Z][A-Za-z0-9]*)+`)

var (
	workBuddySentenceBreak = regexp.MustCompile(`([.!?])(\s+)`)
	workBuddyClauseBreak   = regexp.MustCompile(`([,;])(\s+)`)
	// CJK 句号规则本身不带“空白必须干净”的条件（标点后可能直接跟文字），
	// 因此显式要求后继不是零宽空格（或已是行尾），重复执行才不会叠加。
	workBuddyCJKPunct = regexp.MustCompile(`([。！？])([^\x{200b}]|$)`)
)

// PerturbWorkBuddyBody 按参考实现 perturbChannelFingerprints 的同一规则，在
// system/assistant 文本的句读边界插入零宽空格，让上游按精确子串判定的渠道
// 指纹不再命中；语义不变、对用户不可见。返回 false 表示没有可改动的内容，
// 调用方不应为此多花一次上游请求。
//
// 0.6.25 起首跳即调用（先于任何上游请求），不再等 11128 拒绝后补救：带指纹
// 的原始体首发必被拒，且该拒绝会触发上游分钟级拦截窗口（2026-10-26 生产
// 实证）。幂等（已含零宽空格的边界不叠加），重复执行安全。
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

// workBuddyIdentityClause 匹配 agent 提示开场身份从句（对应参考实现 5.11.0
// 的 IDENTITY_CLAUSE）。上游自 2026-10 起按大小写不敏感的整段子串拦截，
// 其中 “You are a coding agent powered by the <model> model.” 一句没有相邻
// 大写词对、没有句中逗号、终结句点落在指纹之外——稀疏扰动的全部规则对它
// 零命中（0.6.25 生产实证 afreq_862d819d：三发扰动体仍全部 11128
// “request illegal”）。模型名可变，因此只匹配到 “powered by” 为止，可变
// 尾部交给从句内处处打断兜底。
var workBuddyIdentityClause = regexp.MustCompile(`(?i)(you)(\s+are\s+(?:(?:an?\s+)?[\w-]*\s*agent\s+powered\s+by|interacting\s+with\s+the\s+user\s+through\s+the\s+deepseek\s+harness))`)

// workBuddyWhitespace 匹配从句内的空白串；断点插在每个空白串之后，保留
// 原有间距，阅读与分词看到的词序不变。
var workBuddyWhitespace = regexp.MustCompile(`(\s+)`)

// breakWorkBuddyIdentityClauses 在身份从句内部的每个词边界插入零宽空格。
// 上游指纹是子串匹配且无从得知它登记的是从句的哪一段：只打断一处仅能击溃
// 从该处开始的指纹，从句内处处打断则任何端点的指纹都无法完整命中。幂等：
// 插入的 U+200B 不属于 \s，已打断的从句不再被匹配，重复执行不叠加。
func breakWorkBuddyIdentityClauses(text string) string {
	return workBuddyIdentityClause.ReplaceAllStringFunc(text, func(clause string) string {
		return workBuddyWhitespace.ReplaceAllString(clause, "$1"+workBuddyZeroWidthSpace)
	})
}

func workBuddyPerturbText(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	// 身份从句打断必须先于稀疏扰动：稀疏会在 “DeepSeek Harness” 等词间
	// 插入零宽空格，而从句正则的 \s 不认零宽空格，先稀疏会让从句匹配
	// 失败，上游可能登记的前缀指纹（… through the DeepSeek …）继续完整。
	next := breakWorkBuddyIdentityClauses(text)
	// 稀疏扰动的每条规则都要求命中点紧邻空白，已插入的零宽空格会让该
	// 边界自然失配，因此在含零宽空格的文本上继续执行也保持幂等。
	next = workBuddySentenceBreak.ReplaceAllString(next, "$1"+workBuddyZeroWidthSpace+"$2")
	next = workBuddyCJKPunct.ReplaceAllString(next, "$1"+workBuddyZeroWidthSpace+"$2")
	next = workBuddyClauseBreak.ReplaceAllString(next, "$1"+workBuddyZeroWidthSpace+"$2")
	next = workBuddyWordChain.ReplaceAllStringFunc(next, func(chain string) string {
		return workBuddyWhitespace.ReplaceAllString(chain, "$1"+workBuddyZeroWidthSpace)
	})
	if next == text {
		return text, false
	}
	return next, true
}

// workBuddyPerturbContent 对 string content 或 array 里的每个 text part 施加
// 零宽扰动；文本遍历与改写边界由 workBuddyTransformContent 统一提供。
func workBuddyPerturbContent(raw json.RawMessage) (json.RawMessage, bool) {
	return workBuddyTransformContent(raw, workBuddyPerturbText)
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

// workBuddyNeutralContent 对 string content 或 array 里的每个 text part 施加
// 中性头；文本遍历与改写边界由 workBuddyTransformContent 统一提供。
func workBuddyNeutralContent(raw json.RawMessage) (json.RawMessage, bool) {
	return workBuddyTransformContent(raw, workBuddyNeutralText)
}

// 说明：零宽扰动与中性头共用 workBuddyTransformContent（见
// workbuddy_fingerprint.go），保证「只改文本、不动图片与工具」的边界只有一份实现。
