package relay

import "strings"

// probe 在探测状态下判断缓冲区是否已能定论：思考块转入改写，
// 协议标记上报拒绝，普通正文整段放行，仍认不出则继续等下一行。
func (g *streamContentGuard) probe() ([]byte, bool) {
	text := g.buffer.String()
	switch classifyGuardPrefix(text) {
	case guardMarkup:
		return g.reject()
	case guardThinking:
		delta, ok := chatDelta(g.template)
		if !ok {
			g.state = guardDone
			return nil, false
		}
		thinking, answer, closer, _ := splitLeakedThinking(text)
		if answer == "" && !strings.Contains(text, closer) {
			// 本段末尾可能是半个结束标签（上游常把标签拆在两个分片里）：
			// 先留住这段尾巴，否则这里会把半截标签当思考交付，
			// 后面真正的摘要也永远判不出形态。
			if keep := closerTagSuffixLen(thinking, closer); keep > 0 {
				g.tail = thinking[len(thinking)-keep:]
				thinking = thinking[:len(thinking)-keep]
			}
		}
		delete(delta, "content")
		if answer == "" {
			// 思考块还没收起：整段正文继续按思考处理。先暂存不下发，
			// 这样「块收起后其实是摘要信封」时客户端还没收到字节，可以换渠道重试。
			if strings.Contains(text, closer) {
				// 块已收起但后面还没有正文（生产报文里 <analysis> 与 <summary>
				// 常常分在两个分片）：等下一段正文再决定形态，不能直接收工。
				g.buffer.Reset()
				g.state = guardProbing
			} else {
				g.closer = closer
				g.state = guardThinkingInside
			}
			return g.holdThinking(g.template, thinking)
		}
		// 思考块在本段内收起，后面还带着正文：标签之后的正文必须重新按开头判定。
		// 模型跑偏时这里是 <summary>…</summary> 摘要信封，要拦成不可交付；
		// 是真正的回答时把暂存的思考与正文同行交付，思考内容不丢。
		g.buffer.Reset()
		g.buffer.WriteString(answer)
		switch classifyGuardPrefix(answer) {
		case guardMarkup:
			return g.reject()
		case guardNone:
			g.state = guardDone
			// 本段内的思考要先并入暂存，再和正文同行交付，思考内容不丢。
			g.heldBody = g.template
			g.held.WriteString(thinking)
			return g.releaseHeld(g.template, answer)
		}
		// 正文开头还是个标签（或标签的前半截）：思考先暂存，
		// 正文留在缓冲区，等后续分片到齐再判形态。
		g.state = guardProbing
		return g.holdThinking(g.template, thinking)
	case guardUndecided:
		return nil, false
	}
	return g.flushAsContent()
}
