package relay

import "testing"

func TestStreamContentGuardFinishRejectsEmptyAndSplitDrafts(t *testing.T) {
	cases := map[string][]string{
		"空 analysis 草稿":     {"<analysis>"},
		"空 thinking 草稿":     {"<thinking>"},
		"空草稿与拆尾同片":          {"<analysis></anal"},
		"空草稿与拆尾分片":          {"<analysis>", "</anal"},
		"非空草稿拆尾":            {"<analysis>草稿</anal"},
		"空草稿已经闭合":           {"<analysis></analysis>"},
		"空草稿跨片闭合":           {"<analysis>", "</anal", "ysis>"},
		"空 thinking 草稿已经闭合": {"<thinking></thinking>"},
		"空草稿闭合后只有空白":        {"<analysis></analysis>", "\n\n"},
		"空草稿闭合后仍在探测":        {"<analysis></analysis><"},
		"非空草稿已经闭合":          {"<analysis>草稿</analysis>"},
	}
	for name, chunks := range cases {
		t.Run(name, func(t *testing.T) {
			guard := newStreamContentGuard("/chat/completions")
			for _, chunk := range chunks {
				line := []byte(`data: {"choices":[{"index":0,"delta":{"content":` + mustJSON(t, chunk) + `}}]}` + "\n")
				if out, rejected := guard.Observe(line); rejected || out != nil {
					t.Fatalf("草稿应先暂存：rejected=%v out=%s", rejected, out)
				}
			}
			if !guard.Finish() {
				t.Fatal("EOF 时已确认草稿但没有回答，应判为不可交付")
			}
			if guard.state != guardDone || guard.held.Len() != 0 || guard.heldBody != nil || guard.tail != "" || guard.buffer.Len() != 0 {
				t.Fatalf("收尾未清理草稿状态：state=%v held=%q tail=%q buffer=%q", guard.state, guard.held.String(), guard.tail, guard.buffer.String())
			}
			if guard.Finish() {
				t.Fatal("Finish 应幂等，已拒绝的草稿不应重复拒绝")
			}
		})
	}
}

func TestStreamContentGuardFinishKeepsUnconfirmedOrAnsweredContent(t *testing.T) {
	cases := map[string][]string{
		"没有正文":          nil,
		"普通小于号":         {"<"},
		"未确认标签":         {"<anal"},
		"只有空白":          {" \n"},
		"普通回答":          {"合法回答"},
		"合法标签回答":        {"<", "table>表格示例"},
		"空草稿后同片回答":      {"<analysis></analysis>合法回答"},
		"空草稿后跨片回答":      {"<analysis></analysis>", "合法回答"},
		"草稿拆尾闭合后回答":     {"<analysis></anal", "ysis>合法回答"},
		"空草稿后合法小于号开头回答": {"<analysis></analysis>", "<", "table>表格示例"},
	}
	for name, chunks := range cases {
		t.Run(name, func(t *testing.T) {
			guard := newStreamContentGuard("/chat/completions")
			for _, chunk := range chunks {
				line := []byte(`data: {"choices":[{"index":0,"delta":{"content":` + mustJSON(t, chunk) + `}}]}` + "\n")
				if _, rejected := guard.Observe(line); rejected {
					t.Fatal("未确认标签或合法回答不应拒绝")
				}
			}
			if guard.Finish() {
				t.Fatal("没有已确认且未交付的草稿，不应因 EOF 拒绝")
			}
		})
	}
	var disabled *streamContentGuard
	if disabled.Finish() {
		t.Fatal("禁用的守卫不应拒绝")
	}
}

func TestStreamContentGuardRejectHeldAtEndUsesDraftState(t *testing.T) {
	terminal := map[string]any{"choices": []any{map[string]any{"finish_reason": "stop"}}}
	for _, chunk := range []string{"<analysis>", "<analysis></anal", "<analysis></analysis>"} {
		t.Run(chunk, func(t *testing.T) {
			guard := newStreamContentGuard("/chat/completions")
			line := []byte(`data: {"choices":[{"delta":{"content":` + mustJSON(t, chunk) + `}}]}` + "\n")
			guard.Observe(line)
			if out, rejected := guard.rejectHeldAtEnd(terminal); !rejected || out != nil {
				t.Fatalf("terminal 应拒绝未回答的空草稿：rejected=%v out=%s", rejected, out)
			}
		})
	}
}
