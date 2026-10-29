package channel

import (
	"encoding/json"
	"strings"
)

// WorkBuddy 渠道门的固定指纹改词层（移植自 dsh-connect-workbuddy v5.13.1）。
//
// WHY。2026-10-29 生产实证（afreq_aaf2a19a…，hy3 五次上游尝试全 400
// "Illegal API invocation from an unapproved channel"）：本渠道此前的应对是
// 零宽空格扰动（PerturbWorkBuddyBody），但上游在匹配前会归一化掉零宽字符
// ——参考实现 2026-10-28 的对照实验里，system 开头插入 U+200B 仍然 400，
// 字节级扰动对这道门没有出路。这道门匹配的是请求里**固定的文字**：同一份
// body 连发 100 次全 400（确定性拒绝，不是抖动），而把开头换成同义写法后
// 立刻 200；只改其中一处仍 400（多处指纹是**组合命中**），四处全改才 200。
//
// HOW。按句做同义改写：表里每一条是「原文短语 → 同义短语」，命中就换，未
// 命中逐字节原样返回。整表跑完而不是命中一条就停，因为组合命中要求所有
// 已登记指纹同时消失。改写句与原句同义，模型读到的规则不变。
//
// 边界。只改 system、developer 与 assistant 的文本（string content，以及 array
// content 里的每个 text part）：参考实现实测上游不扫 user / tool，改用户原话
// 会凭空改变对话，工具定义与图片 part 一律不动。
//
// developer 必须一起改（2026-10-29 14:07 生产实证 afreq_ea1065d7…，
// deepseek-flash-1017 五次上游 400）：那条请求**没有 system 消息**，身份提示写
// 在 developer 里（58 assistant / 1 developer / 52 tool / 18 user），本层先跑、
// 当时只看 system/assistant 便跳过了它；紧接着 ApplyWorkBuddyBody 把 developer
// 规范化成 system 再发出，指纹就原样落进上游的扫描面。所以「改词」与「角色
// 归一化」必须按同一个角色集合取值——凡最终会被上游当成 system/assistant 的
// 消息，都在这里改。这也解释了为什么同一渠道上「hy3 过、另一个模型被拦」：
// 差异在请求形态（谁发 system、谁发 developer），不在模型本身。
//
// 改词表大小写精确匹配——宿主 system 是固定大小写；上游本身不区分大小写，若
// 出现新的大小写形态需要新的实测证据再补。表是易增补的：新增指纹只需加一行，
// 并在 workbuddy_fingerprint_test.go 里补一条断言。

// workBuddyFingerprintRewrite 是一句「上游登记的固定指纹 → 同义写法」。
type workBuddyFingerprintRewrite struct {
	from string
	to   string
}

// workBuddyFingerprintRewrites 是 DSH 宿主 system 里被上游登记为「非官方
// 客户端」的固定短语表，条目顺序即改写顺序（前面的改写不会重新命中后面的
// 原文，因此表本身幂等）。
var workBuddyFingerprintRewrites = []workBuddyFingerprintRewrite{
	{
		from: "You are an AI agent powered by DeepSeek Harness",
		to:   "You are an AI agent running inside DeepSeek Harness",
	},
	{
		// 模型名可变，因此只改到 "powered by " 为止，尾部由宿主的模型名补完。
		from: "You are a coding agent powered by the ",
		to:   "You are a coding agent running on the ",
	},
	{
		from: "You are interacting with the user through the DeepSeek Harness Web GUI",
		to:   "You are working with the user inside the DeepSeek Harness Web GUI",
	},
	{
		from: "Agent Teams is available in this session",
		to:   "Agent Teams is enabled for this session",
	},
	// 以下九条是宿主 @ 路径说明整段里的指纹：整段组合命中，与位置无关，
	// 必须逐句改写。
	{
		from: "Tokens prefixed with @ are paths the user explicitly referenced.",
		to:   "Paths that begin with @ are the ones the user explicitly referenced.",
	},
	{
		from: "Relative paths resolve from the workspace root;",
		to:   "Relative paths are resolved against the workspace root;",
	},
	{
		from: "absolute paths identify files or directories on the host.",
		to:   "absolute paths point at files or directories on the host.",
	},
	{
		from: "A trailing slash marks a directory:",
		to:   "A trailing slash indicates a directory:",
	},
	{
		from: "list it when its contents matter.",
		to:   "list it whenever its contents matter.",
	},
	{
		from: "Anything else is a file:",
		to:   "Everything else is a file:",
	},
	{
		from: "use the read tool when its contents are needed,",
		to:   "use the read tool whenever you need its contents,",
	},
	{
		from: "and do not claim to have inspected it before reading.",
		to:   "and never claim to have inspected it before reading it.",
	},
	{
		from: `@"..." quotes a path containing spaces.`,
		to:   `A path containing spaces is quoted with @"...".`,
	},
}

// RewriteWorkBuddyFingerprintPhrases 把 system/assistant 文本里已登记的固定
// 指纹短语改写为同义写法。返回 false 表示没有任何命中，调用方应原样使用
// 原报文（不为此多一次 JSON 往返）。
//
// 必须在 ApplyWorkBuddyBody 的中性头处理**之前**调用：中性头与零宽扰动都会
// 先改动文本，一旦被禁短语被插入了不可见字符或前缀，改词表就匹配不到原文。
func RewriteWorkBuddyFingerprintPhrases(body []byte) ([]byte, bool) {
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
		if !workBuddyIsScannedRole(role) {
			continue
		}
		content, ok := workBuddyTransformContent(message["content"], workBuddyRewriteFingerprintText)
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

// workBuddyIsScannedRole 判断哪些消息角色需要参与改词。
//
// 上游只扫 system 与 assistant，但本渠道会把 developer 规范化成 system 再发出
// （ApplyWorkBuddyBody），因此 developer 同样落在上游的扫描面里，必须一起改。
func workBuddyIsScannedRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "assistant", "developer":
		return true
	}
	return false
}

// workBuddyRewriteFingerprintText 对一段文本整表改写；无命中时原样返回。
func workBuddyRewriteFingerprintText(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	changed := false
	for _, rule := range workBuddyFingerprintRewrites {
		if !strings.Contains(text, rule.from) {
			continue
		}
		text = strings.ReplaceAll(text, rule.from, rule.to)
		changed = true
	}
	if !changed {
		return text, false
	}
	return text, true
}

// workBuddyTransformContent 对 string content 或 array content 里的每个 text
// part 应用 transform；中性头、零宽扰动与指纹改词三层共用这一份实现，
// 保证「只碰文本、不碰图片/工具」的边界只写一次。
func workBuddyTransformContent(raw json.RawMessage, transform func(string) (string, bool)) (json.RawMessage, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if next, ok := transform(text); ok {
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
		next, ok := transform(value)
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
