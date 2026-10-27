package relay

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// 正文污染守卫（content guard）。
//
// 生产报文里有两类「标签写进正文」的污染，客户端都会把它当成正常回答存进会话
// 历史，下一轮再原样回传；上游于是继续按这个坏形态输出，最终表现为对话在没有
// 回答的情况下中断（实证见 2026-10-27 生产报文）。
//
//	1. 思考块泄漏：上游把整段思考用 <thinking>…</thinking> 包进正文，而不是放进
//	   reasoning_content（adesk 渠道 deepseek 系列实证）。
//	2. 客户端内部标记泄漏：上游把自家客户端/Agent 框架的内部标记当正文写出，例如
//	   <tool_result name=read> 整块文件内容、<tool_call(s)>/<tool invoke>、｜…｜ 形式的
//	   DSML 收尾标记（workbuddy 渠道实证），以及 Agent 框架上下文压缩用的
//	   <summary>…</summary> 摘要信封（adesk 渠道实证）。
//
// 处理规则不区分渠道和模型，全部响应共用一条：
//   - 思考块：搬回 reasoning_content、剥掉标签，正文只留真正的回答，可以安全交付；
//   - 协议标记：无法还原成正确的工具调用，判定为上游坏响应、不交付给客户端，
//     交回候选循环切换或重试，避免垃圾写进客户端历史。
//
// 两类判定都只看正文开头的文本形态，且只在正文第一次出现时判断一次；判定不成立
// 就立刻按普通正文放行，不影响正常的流式行为。

const (
	// guardProbeLimit 限制探测窗口：攒够这么多字节还认不出形态，就按普通正文放行，
	// 避免把以 "<" 开头的正常文本长期压在缓冲区里。
	guardProbeLimit = 24
	// leakCloser 是思考块的结束标签；上游连结束标签都没给时，整段正文都算思考。
	leakCloser = "</thinking>"
)

// leakOpeners 是观察到的思考块开头写法。只认这几种，避免误伤 "<" 开头的普通文本。
var leakOpeners = []string{"<thinking>", "<thinking ", "<thinking:", "<thinkingState:"}

// markupLeakPrefixes 是上游自家客户端/Agent 框架的内部标记开头。这些标记对任何
// OpenAI 兼容客户端都不是合法正文，出现即说明这条响应不能直接交付。
//
// 其中 <summary> 来自 Agent 框架的上下文压缩格式：客户端把 checkpoint（含
// <compacted-summary> 段落）当作一条 user 消息塞进请求后，上游模型会照抄这个格式，
// 把新写的一段摘要当成本轮回答（生产实证 afreq_04b72bca01aaab19292942ab）。
// 压缩结果本身用 <compacted-summary> 承载，这里刻意不拦，避免把正常的上下文压缩打断。
var markupLeakPrefixes = []string{
	"<tool_result", "<tool_calls>", "<tool_calls ", "<tool_call>", "<tool_call ",
	"<tool invoke", "<function_results", "<function_calls", "<｜", "｜DSML｜", "<|tool",
	"<summary>",
}

// guardDecision 是对正文开头形态的判定结果。
type guardDecision int

const (
	// guardUndecided 还认不出，需要继续攒字节；guardNone 判定为普通正文。
	guardUndecided guardDecision = iota
	guardNone
	guardThinking
	guardMarkup
)

// classifyGuardPrefix 判定一段正文开头的形态。
func classifyGuardPrefix(text string) guardDecision {
	if _, ok := stripLeakOpener(text); ok {
		return guardThinking
	}
	for _, prefix := range markupLeakPrefixes {
		if strings.HasPrefix(text, prefix) {
			return guardMarkup
		}
	}
	if isLeakOpenerPrefix(text) || isMarkupPrefix(text) {
		return guardUndecided
	}
	return guardNone
}

// isMarkupPrefix 判断文本是否仍是某个协议标记的**前缀**（分片下发时先到一半）。
func isMarkupPrefix(text string) bool {
	for _, prefix := range markupLeakPrefixes {
		if strings.HasPrefix(prefix, text) {
			return true
		}
	}
	return false
}

// splitLeakedThinking 把一段以思考块开头的文本拆成思考内容与正文。
// 第三个返回值表示这段文本确实以思考块开头；结束时剥掉结束标签并去掉其后的空白。
func splitLeakedThinking(text string) (thinking, content string, ok bool) {
	rest, ok := stripLeakOpener(text)
	if !ok {
		return "", "", false
	}
	if closer := strings.Index(rest, leakCloser); closer >= 0 {
		return rest[:closer], strings.TrimLeft(rest[closer+len(leakCloser):], " \t\r\n"), true
	}
	return rest, "", true
}

// stripLeakOpener 判断文本是否以完整思考块开头，并返回去掉开头后的剩余文本。
func stripLeakOpener(text string) (string, bool) {
	for _, opener := range leakOpeners {
		if strings.HasPrefix(text, opener) {
			return text[len(opener):], true
		}
	}
	return text, false
}

// isLeakOpenerPrefix 判断文本是否仍是某个思考块开头的**前缀**（含完整开头）。
func isLeakOpenerPrefix(text string) bool {
	for _, opener := range leakOpeners {
		if strings.HasPrefix(opener, text) {
			return true
		}
	}
	return false
}

// inspectBufferedContent 处理非流式 chat.completion 响应：先修思考块泄漏，
// 再把「正文整体就是客户端协议标记」的响应标记出来交给上层次处理。
// 第二个返回值表示该响应应当按上游失败处理，不再交付给客户端。
func inspectBufferedContent(endpoint string, body []byte) ([]byte, bool) {
	if endpoint != protocol.ChatCompletionsEndpoint || !bytes.Contains(body, []byte("<")) {
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
			if hasReasoning(message) {
				continue
			}
			thinking, answer, ok := splitLeakedThinking(strings.TrimLeft(content, " \t\r\n"))
			if !ok {
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

// streamContentGuard 是流式响应的状态机：只在正文第一次出现时判定一次，
// 认出思考块后把后续正文改写成 reasoning_content，认出协议标记则上报拒绝。
type streamContentGuard struct {
	state    guardState
	buffer   strings.Builder
	// template 保存判定所在那一行的报文，改写思考内容时以它为骨架，
	// 保留 index/role/finish_reason 等与正文无关的字段。
	template map[string]any
}

type guardState int

const (
	// guardIdle 还没遇到正文；guardProbing 正在判断开头；
	// guardThinkingInside 已确认是思考块；guardDone 本次响应不再处理。
	guardIdle guardState = iota
	guardProbing
	guardThinkingInside
	guardDone
)

// newStreamContentGuard 只为 Chat Completions 上游启用：其它协议端点的正文结构
// 不同，判定规则不适用，统一按原样透传。
func newStreamContentGuard(endpoint string) *streamContentGuard {
	if endpoint != protocol.ChatCompletionsEndpoint {
		return nil
	}
	return &streamContentGuard{}
}

// Observe 处理一行上游 SSE，返回应当转发的行（可能已改写）以及是否拒绝该响应。
// 返回 nil 行表示该行已被吸收进探测缓冲，本次不向客户端写出。
func (g *streamContentGuard) Observe(line []byte) ([]byte, bool) {
	if g == nil || g.state == guardDone {
		return line, false
	}
	payload, done, valid := relaySSEDataPayload(line)
	if !valid || done {
		return line, false
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return line, false
	}
	delta, ok := chatDelta(body)
	if !ok {
		return line, false
	}
	if hasReasoning(delta) {
		// 上游已经按标准字段分离思考，这条响应不需要修复。
		g.state = guardDone
		return line, false
	}
	content, _ := delta["content"].(string)
	switch g.state {
	case guardIdle:
		// 首包常常只带 role、正文为空，空正文不算「已经判过」，
		// 要继续等真正的第一段正文再决定。
		if content == "" {
			return line, false
		}
		switch classifyGuardPrefix(content) {
		case guardNone:
			g.state = guardDone
			return line, false
		case guardMarkup:
			g.state = guardDone
			return nil, true
		}
		g.state = guardProbing
		g.template = body
		g.buffer.WriteString(content)
		return g.probe()
	case guardProbing:
		g.buffer.WriteString(content)
		if g.buffer.Len() > guardProbeLimit {
			return g.flushAsContent()
		}
		return g.probe()
	}
	// guardThinkingInside：正文都属于思考，直到出现结束标签为止。
	if content == "" {
		return line, false
	}
	if closer := strings.Index(content, leakCloser); closer >= 0 {
		g.state = guardDone
		delta[reasoningFieldStandard] = content[:closer]
		delta["content"] = strings.TrimLeft(content[closer+len(leakCloser):], " \t\r\n")
		return encodeGuardLine(body), false
	}
	delete(delta, "content")
	delta[reasoningFieldStandard] = content
	return encodeGuardLine(body), false
}

// probe 在探测状态下判断缓冲区是否已能定论：思考块转入改写，
// 协议标记上报拒绝，普通正文整段放行，仍认不出则继续等下一行。
func (g *streamContentGuard) probe() ([]byte, bool) {
	text := g.buffer.String()
	switch classifyGuardPrefix(text) {
	case guardMarkup:
		g.state = guardDone
		return nil, true
	case guardThinking:
		delta, ok := chatDelta(g.template)
		if !ok {
			g.state = guardDone
			return nil, false
		}
		thinking, answer, _ := splitLeakedThinking(text)
		delta[reasoningFieldStandard] = thinking
		// 缓冲里已经出现结束标签，说明思考块在这一行就结束，后续正文按普通正文处理。
		if strings.Contains(text, leakCloser) {
			g.state = guardDone
		} else {
			g.state = guardThinkingInside
		}
		if answer == "" {
			delete(delta, "content")
		} else {
			delta["content"] = answer
		}
		return encodeGuardLine(g.template), false
	case guardUndecided:
		return nil, false
	}
	return g.flushAsContent()
}

// flushAsContent 把缓冲区按普通正文原样交还，并结束本次探测。
func (g *streamContentGuard) flushAsContent() ([]byte, bool) {
	g.state = guardDone
	delta, ok := chatDelta(g.template)
	if !ok {
		return nil, false
	}
	delta["content"] = g.buffer.String()
	delete(delta, reasoningFieldStandard)
	return encodeGuardLine(g.template), false
}

// chatDelta 取出 chat.completion(.chunk) 里第一个 choice 的 delta。
func chatDelta(body map[string]any) (map[string]any, bool) {
	choices, ok := body["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, false
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, false
	}
	delta, ok := choice["delta"].(map[string]any)
	return delta, ok
}

// hasReasoning 判断 message/delta 里是否已经有非空思考内容。
func hasReasoning(container map[string]any) bool {
	for _, field := range []string{reasoningFieldStandard, reasoningFieldCompact, "thinking"} {
		if text, ok := container[field].(string); ok && text != "" {
			return true
		}
	}
	return false
}

// encodeGuardLine 把改写后的报文重新拼成一行 SSE。
func encodeGuardLine(body map[string]any) []byte {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	return []byte("data: " + string(payload) + "\n")
}