package relay


// 暂存与补发：思考块已识别、但「后面跟的是不是回答」还没定论时，先把这段正文留在
// 网关，不下发给客户端。这样一旦判出摘要信封，客户端还没收到任何字节，候选循环
// 可以直接换渠道重试，不必在流中间报 502。超过上限则退回旧语义：立刻下发。
const guardHeldThinkingLimit = 16 << 10
// holdThinking 暂存已判定为思考的正文，暂不下发给客户端。
// 这样「思考块收起后跟着摘要信封」时客户端还没收到任何字节，候选循环可以直接
// 换渠道重试，不必在流中间报错。超过上限则退回旧语义：立刻下发，避免无限缓冲。
func (g *streamContentGuard) holdThinking(body map[string]any, thinking string) ([]byte, bool) {
	if thinking == "" {
		return nil, false
	}
	g.heldBody = body
	g.held.WriteString(thinking)
	if g.held.Len() <= guardHeldThinkingLimit {
		return nil, false
	}
	return g.releaseHeld(body, "")
}

// releaseHeld 把暂存的思考与（可选的）正文合并成一行下发。
// answer 为空表示只补发思考；body 优先用当前行，缺 delta 时退回暂存时的骨架。
func (g *streamContentGuard) releaseHeld(body map[string]any, answer string) ([]byte, bool) {
	thinking := g.held.String()
	g.held.Reset()
	if thinking == "" && answer == "" {
		g.heldBody = nil
		return nil, false
	}
	target := body
	if _, ok := chatDelta(target); !ok {
		target = g.heldBody
	}
	g.heldBody = nil
	if target == nil {
		return nil, false
	}
	delta, ok := chatDelta(target)
	if !ok {
		return nil, false
	}
	if thinking != "" {
		mergeGuardReasoning(delta, thinking)
	}
	if answer != "" {
		delta["content"] = answer
	} else {
		delete(delta, "content")
	}
	return encodeGuardLine(target), false
}

// guardStreamTerminal 判断这一行是否为收尾行（上游已给出 finish_reason）。
func guardStreamTerminal(body map[string]any) bool {
	choices, ok := body["choices"].([]any)
	if !ok {
		return false
	}
	for _, item := range choices {
		choice, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch finish := choice["finish_reason"].(type) {
		case string:
			if finish != "" {
				return true
			}
		case nil:
		default:
			// finish_reason 非字符串也视为已收尾，宁可判结束也不无限等。
			return true
		}
	}
	return false
}

// rejectHeldAtEnd 处理收尾行：到流结束时还只剩思考、没给过任何回答，说明这一轮
// 模型没产出可交付的回答（典型是草稿块一直没收起）。判为不可交付交回候选循环
// 换渠道重试，避免把「没有回答」的一轮当成成功响应交给客户端。
func (g *streamContentGuard) rejectHeldAtEnd(body map[string]any) ([]byte, bool) {
	if g.held.Len() == 0 || !guardStreamTerminal(body) {
		return nil, false
	}
	return g.reject()
}

// Finish 在流扫描结束后收尾：还在暂存思考同样是「只有草稿没有回答」，
// 与收尾行判定一致上报不可交付（上游可能连 finish_reason 都没给）。
func (g *streamContentGuard) Finish() bool {
	if g == nil || g.held.Len() == 0 {
		return false
	}
	g.held.Reset()
	g.heldBody = nil
	return true
}
// reject 上报「这条响应不可交付」：暂存的思考一并丢弃。
// 关键点：判定阶段一个字节都没写给客户端，候选循环才有机会换渠道重试。
func (g *streamContentGuard) reject() ([]byte, bool) {
	g.state = guardDone
	g.held.Reset()
	g.heldBody = nil
	return nil, true
}