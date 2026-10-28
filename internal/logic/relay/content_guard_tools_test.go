package relay

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tidwall/gjson"
)

func TestStreamContentGuardReleasesHeldWithStandardReasoningAnswer(t *testing.T) {
	for _, field := range []string{reasoningFieldStandard, reasoningFieldCompact, "thinking"} {
		t.Run(field, func(t *testing.T) {
			guard := newStreamContentGuard("/chat/completions")
			draft := `data: {"choices":[{"index":0,"delta":{"content":"<analysis>A</analysis>"}}]}` + "\n"
			if out, rejected := guard.Observe([]byte(draft)); rejected || out != nil {
				t.Fatalf("草稿应暂存：rejected=%v out=%s", rejected, out)
			}
			line := `data: {"id":"answer","choices":[{"index":0,"delta":{"role":"assistant","content":"正常回答","` + field + `":"B"},"finish_reason":"stop"}]}` + "\n"
			out, rejected := guard.Observe([]byte(line))
			if rejected || out == nil {
				t.Fatalf("合法回答不可拒绝：rejected=%v out=%s", rejected, out)
			}
			payload := out[len("data: "):]
			want := "A"
			if field == reasoningFieldStandard {
				want = "AB"
			}
			if got := gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String(); got != want {
				t.Fatalf("reasoning_content=%q, want=%q", got, want)
			}
			if field != reasoningFieldStandard && gjson.GetBytes(payload, "choices.0.delta."+field).String() != "B" {
				t.Fatalf("上游思考字段丢失：%s", out)
			}
			if gjson.GetBytes(payload, "choices.0.delta.content").String() != "正常回答" || gjson.GetBytes(payload, "choices.0.finish_reason").String() != "stop" {
				t.Fatalf("回答或结束字段丢失：%s", out)
			}
			if guard.held.Len() != 0 || guard.Finish() {
				t.Fatal("合法回答交付后不可在 Finish 再拒绝")
			}
		})
	}
}

func TestStreamContentGuardPreservesToolCallsAndReasoning(t *testing.T) {
	cases := []struct {
		name, draft, content, wantReasoning, wantContent string
	}{
		{"暂存后工具", "<analysis>A</analysis>", "", "AB", ""},
		{"暂存未闭合后工具", "<analysis>A", "", "AB", ""},
		{"同片草稿工具", "", "<analysis>A</analysis>", "AB", ""},
		{"同片未闭合草稿工具", "", "<thinking>A", "AB", ""},
		{"同片草稿回答工具", "", "<analysis>A</analysis>回答", "AB", "回答"},
		{"思考中工具和正文", "<analysis>A", "C</analysis>回答", "ACB", "回答"},
		{"半截结束标签", "<analysis>A</anal", "ysis>", "AB", ""},
		{"暂存后工具标记", "<analysis>A</analysis>", "<summary>草稿</summary>", "AB", ""},
		{"探测前缀后工具", "<anal", "ysis>草稿", "草稿B", ""},
		{"正常正文工具", "", "回答", "B", "回答"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guard := newStreamContentGuard("/chat/completions")
			if tc.draft != "" {
				out, rejected := guard.Observe([]byte(`data: {"choices":[{"index":0,"delta":{"content":` + mustJSON(t, tc.draft) + `}}]}` + "\n"))
				if out != nil || rejected {
					t.Fatalf("草稿应暂存：rejected=%v out=%s", rejected, out)
				}
			}
			line := `data: {"id":"tool","extra":{"a":1},"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"B","reasoning":"compact","thinking":"native","extra":[1,2],"content":` + mustJSON(t, tc.content) + `,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n"
			out, rejected := guard.Observe([]byte(line))
			if out == nil || rejected {
				t.Fatalf("合法工具行必须交付：rejected=%v out=%s", rejected, out)
			}
			var before, after map[string]any
			if err := json.Unmarshal([]byte(line[len("data: "):]), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(out[len("data: "):], &after); err != nil {
				t.Fatal(err)
			}
			delta, _ := chatDelta(before)
			delta[reasoningFieldStandard] = tc.wantReasoning
			if tc.wantContent == "" {
				delete(delta, "content")
			} else {
				delta["content"] = tc.wantContent
			}
			// 不强制删除上游本就为空的 content；比较所有其它字段，含完整工具参数。
			actualDelta, _ := chatDelta(after)
			if tc.wantContent == "" && actualDelta["content"] == "" {
				delete(actualDelta, "content")
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("仅能改动 content/reasoning_content：\nwant=%#v\ngot=%#v", before, after)
			}
			if guard.held.Len() != 0 || guard.Finish() {
				t.Fatal("工具调用交付后不可在 Finish 再拒绝")
			}
			terminal := []byte(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n")
			if got, rejected := guard.Observe(terminal); rejected || string(got) != string(terminal) {
				t.Fatalf("工具回合结束应直通：rejected=%v out=%s", rejected, got)
			}
		})
	}
}

func TestStreamContentGuardThinkingDeltaDoesNotSwallowToolCalls(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	line := []byte(`data: {"choices":[{"delta":{"content":"<thinking>A</thinking>","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]}}]}` + "\n")
	out, rejected := guard.Observe(line)
	if rejected || out == nil || guard.Finish() {
		t.Fatalf("同片草稿不能吞掉工具调用：rejected=%v out=%s", rejected, out)
	}
	payload := out[len("data: "):]
	if gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String() != "A" || gjson.GetBytes(payload, "choices.0.delta.tool_calls.0.id").String() != "call_1" || gjson.GetBytes(payload, "choices.0.delta.content").Exists() {
		t.Fatalf("应保留工具调用并移除正文草稿：%s", out)
	}
}

func TestStreamContentGuardToolArgumentFragmentReleasesHeld(t *testing.T) {
	guard := newStreamContentGuard("/chat/completions")
	guard.Observe([]byte(`data: {"choices":[{"delta":{"content":"<analysis>A</analysis>"}}]}` + "\n"))
	line := []byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}` + "\n")
	out, rejected := guard.Observe(line)
	if rejected || out == nil || guard.Finish() {
		t.Fatalf("参数分片也是合法工具输出：rejected=%v out=%s", rejected, out)
	}
	payload := out[len("data: "):]
	if gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String() != "A" || gjson.GetBytes(payload, "choices.0.delta.tool_calls.0.function.arguments").String() != "}" {
		t.Fatalf("暂存思考或工具参数丢失：%s", out)
	}
}

func TestStreamContentGuardInvalidToolCallsDoNotReleaseDraft(t *testing.T) {
	for _, calls := range []string{`null`, `[]`, `"tool"`, `{}`, `[null]`, `[1]`, `[{}]`} {
		t.Run(calls, func(t *testing.T) {
			guard := newStreamContentGuard("/chat/completions")
			guard.Observe([]byte(`data: {"choices":[{"delta":{"content":"<analysis>A</analysis>"}}]}` + "\n"))
			line := `data: {"choices":[{"delta":{"tool_calls":` + calls + `},"finish_reason":"stop"}]}` + "\n"
			if _, rejected := guard.Observe([]byte(line)); !rejected {
				t.Fatal("无效 tool_calls 不能把只有草稿的一轮伪装为合法输出")
			}
		})
	}
}
