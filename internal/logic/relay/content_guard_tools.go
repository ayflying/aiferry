package relay

import "strings"

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

// releaseToolCall 优先交付结构化工具调用：当前行作骨架，不能再被草稿缓冲吸收。
// 正文中的草稿搬进思考，协议标记/未完成的标签丢弃，但其它 delta 字段全部保留。
func (g *streamContentGuard) releaseToolCall(line []byte, body, delta map[string]any) ([]byte, bool) {
	content, _ := delta["content"].(string)
	answer := content
	thinking := ""
	if g.state == guardThinkingInside {
		thinking = g.tail + content
		answer = ""
		closer := g.closer
		if closer == "" {
			closer = leakCloser
		}
		if at := strings.Index(thinking, closer); at >= 0 {
			answer = strings.TrimLeft(thinking[at+len(closer):], " \t\r\n")
			thinking = thinking[:at]
		}
	} else {
		if g.state == guardProbing {
			answer = g.buffer.String() + content
		}
		if classifyGuardPrefix(answer) == guardThinking {
			thinking, answer, _, _ = splitLeakedThinking(strings.TrimLeft(answer, " \t\r\n"))
		}
	}
	if classifyGuardPrefix(answer) != guardNone {
		answer = ""
	}
	g.held.WriteString(thinking)
	g.state = guardDone
	g.buffer.Reset()
	g.tail = ""
	if g.held.Len() == 0 && answer == content {
		return line, false
	}
	// releaseHeld 在两者都为空时不会编码，仍需保留当前行的工具/标准思考字段。
	if g.held.Len() == 0 && answer == "" {
		delete(delta, "content")
		return encodeGuardLine(body), false
	}
	return g.releaseHeld(body, answer)
}
