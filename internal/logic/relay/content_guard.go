package relay

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// 协议标记守卫（content guard）。
//
// 【已删除的一半，2026-10-29】原先这里还有一套「思考块改写」：上游把整段思考用
// <thinking>…</thinking> 或 <analysis>…</analysis> 包进正文时，网关把它搬进
// reasoning_content、剥掉标签，正文只留真正的回答。那是按 adesk 渠道 deepseek
// 系列的实证加的模型专用补丁。用户 2026-10-29 确认上游已修复标签错乱，网关不再为
// 这类模型做特殊处理，整套分支已删除（实现见 git 历史 58319ba 之前的
// content_guard_thinking.go / content_guard_probe.go / content_guard_hold.go）。
// 现在上游若仍把思考标签写进正文，网关按普通正文原样透传：不改写、不搬移、
// 不因它拒答。
//
// 【保留的一半与模型无关】上游把自家客户端 / Agent 框架的内部标记当正文写出：
// <tool_result name=read> 整块文件内容、<tool_call(s)>、<tool invoke>、｜…｜ 形式的
// DSML 收尾标记（workbuddy 渠道实证），以及 Agent 框架上下文压缩用的
// <summary>…</summary> 摘要信封（adesk 渠道 afreq_04b72bca01aaab19292942ab 实证：
// 客户端把压缩结果当论文塞进请求，模型照抄格式当成本轮回答）。
//
// 这类内容既不能当回答交付——客户端解析失败会把垃圾存进会话历史，下一轮再原样回传，
// 上游于是继续按坏形态输出，最终表现为对话在没有回答的情况下中断（2026-10-27 生产
// 实证）——也还原不成正确的工具调用，所以判定为上游坏响应、不交付给客户端，交回候选
// 循环切换或重试。
//
// 判定只看正文开头的文本形态，且只在正文第一次出现时判断一次；判定不成立立刻按普通
// 正文放行，不影响正常流式行为。

const (
	// guardProbeLimit 限制探测窗口：攒够这么多字节还认不出形态，就按普通正文放行，
	// 避免把以 "<" 开头的正常文本长期压在缓冲区里。
	guardProbeLimit = 24
)

// markupLeakPrefixes 是上游自家客户端 / Agent 框架的内部标记开头。这些标记对任何
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
	// guardUndecided 还认不出，需要继续攒字节；guardNone 判定为普通正文；
	// guardMarkup 判定为协议标记。
	guardUndecided guardDecision = iota
	guardNone
	guardMarkup
)

// classifyGuardPrefix 判定一段正文开头的形态。
//
// 先跳过前导空白再判定：生产报文里协议标记常常落在新分片的开头，不跳过空白就会把
// 紧接在换行之后的标记当成普通正文放行。
func classifyGuardPrefix(raw string) guardDecision {
	text := strings.TrimLeft(raw, " \t\r\n")
	if text == "" {
		return guardUndecided
	}
	for _, prefix := range markupLeakPrefixes {
		if strings.HasPrefix(text, prefix) {
			return guardMarkup
		}
	}
	if isMarkupPrefix(text) {
		return guardUndecided
	}
	return guardNone
}

// hasAngleBracket 判断报文里是否可能出现标签形态的正文。
// 除了字面 "<"，还要认转义写法：部分上游（以及 Go 的 json.Marshal 默认行为）
// 会把尖括号写成 \u003c / \u003C，只看字面 "<" 会让整条响应跳过判定。
func hasAngleBracket(body []byte) bool {
	if bytes.Contains(body, []byte("<")) {
		return true
	}
	return bytes.Contains(body, []byte(`\u003c`)) || bytes.Contains(body, []byte(`\u003C`))
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

// streamContentGuard 是流式响应的状态机：只在正文第一次出现时判定一次，
// 认出协议标记即上报拒绝，其余情况一律原样放行。
type streamContentGuard struct {
	state guardState
	// buffer 暂存判定窗口内的正文：分片下发时标记可能被切成两半，
	// 前面那半必须先留住，等后面的字节到齐再判，否则半截标记会被当成普通正文放行。
	buffer strings.Builder
	// template 保存判定所在那一行的报文，补发缓冲正文时以它为骨架，
	// 保留 index/role/finish_reason 等与正文无关的字段。
	template map[string]any
}

type guardState int

const (
	// guardIdle 还没遇到正文；guardProbing 正在攒判定窗口；
	// guardDone 本次响应不再处理。
	guardIdle guardState = iota
	guardProbing
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
// 返回 nil 行表示该行已被吸收进判定缓冲，本次不向客户端写出。
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
	if hasGuardToolCalls(delta) {
		// 工具调用到达即说明本轮正文已经结束，判定窗口里的字节要就地收尾。
		return g.settleToolCall(line, body, delta)
	}
	content, _ := delta["content"].(string)
	if content == "" {
		// 首包常常只带 role、正文为空，空正文不算「已经判过」，继续等真正的正文。
		return line, false
	}
	switch g.state {
	case guardIdle:
		switch classifyGuardPrefix(content) {
		case guardMarkup:
			return g.reject()
		case guardNone:
			g.state = guardDone
			return line, false
		}
		g.state = guardProbing
		g.template = body
		g.buffer.WriteString(content)
		return g.probe()
	case guardProbing:
		g.buffer.WriteString(content)
		return g.probe()
	}
	return line, false
}

// probe 在探测状态下判断缓冲区是否已能定论：认出协议标记就拒绝，确认是普通正文就把
// 攒下的字节一次性补发，仍认不出且未超出窗口则继续等下一行。
func (g *streamContentGuard) probe() ([]byte, bool) {
	switch classifyGuardPrefix(g.buffer.String()) {
	case guardMarkup:
		return g.reject()
	case guardNone:
		return g.flushAsContent()
	}
	if g.buffer.Len() > guardProbeLimit {
		// 攒够窗口仍认不出形态（以 "<" 开头的普通文本），按普通正文放行。
		return g.flushAsContent()
	}
	return nil, false
}

// settleToolCall 结束正文判定：探测窗口里的字节并到工具调用这一行一起交付；
// 若这段字节其实是协议标记，整条响应仍然不可交付。
func (g *streamContentGuard) settleToolCall(line []byte, body, delta map[string]any) ([]byte, bool) {
	if g.state != guardProbing {
		g.state = guardDone
		return line, false
	}
	buffered := g.buffer.String()
	g.buffer.Reset()
	if classifyGuardPrefix(buffered) == guardMarkup {
		return g.reject()
	}
	g.state = guardDone
	if buffered == "" {
		return line, false
	}
	content, _ := delta["content"].(string)
	delta["content"] = buffered + content
	return encodeGuardLine(body), false
}

// flushAsContent 把判定窗口里攒下的字节当成普通正文补发一行，本次响应不再处理。
func (g *streamContentGuard) flushAsContent() ([]byte, bool) {
	g.state = guardDone
	delta, ok := chatDelta(g.template)
	if !ok {
		return nil, false
	}
	delta["content"] = g.buffer.String()
	return encodeGuardLine(g.template), false
}

// hasGuardToolCalls 只接受非空对象数组；不要求首片的 id/name，后续参数分片也合法。
// 空数组、null 或标量不能把只有草稿的一轮伪装成工具回合。
func hasGuardToolCalls(delta map[string]any) bool {
	calls, ok := delta["tool_calls"].([]any)
	if !ok || len(calls) == 0 {
		return false
	}
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok || len(call) == 0 {
			return false
		}
	}
	return true
}

// reject 上报「这条响应不可交付」。关键点：判定阶段一个字节都没写给客户端，
// 候选循环才有机会换渠道重试。
func (g *streamContentGuard) reject() ([]byte, bool) {
	g.state = guardDone
	g.buffer.Reset()
	g.template = nil
	return nil, true
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

// encodeGuardLine 把改写后的报文重新拼成一行 SSE。
func encodeGuardLine(body map[string]any) []byte {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	return []byte("data: " + string(payload) + "\n")
}
