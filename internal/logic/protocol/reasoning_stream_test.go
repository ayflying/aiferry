package protocol

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// collectStream 把转换出来的 SSE 分片拼成一段文本，方便断言。
func collectStream(converter *StreamConverter, lines []string) string {
	var out strings.Builder
	for _, line := range lines {
		for _, chunk := range converter.Transform([]byte(line)) {
			out.Write(chunk)
			out.WriteString("\n")
		}
	}
	for _, chunk := range converter.Complete() {
		out.Write(chunk)
		out.WriteString("\n")
	}
	return out.String()
}

// dataLines 从 SSE 文本里取出所有 data: payload（跳过 [DONE] 与空行）。
func dataLines(stream string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(stream, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		result = append(result, payload)
	}
	return result
}

// 客户端是 Chat、上游是 Responses（chat_to_responses）：上游的 Responses 思考事件
// 必须落成 Chat 的 reasoning_content 增量。
func TestResponsesReasoningDeltasBecomeChatReasoningContent(t *testing.T) {
	plan, ok := fallbackPlan(ChatCompletionsEndpoint)
	if !ok {
		t.Fatal("chat fallback plan missing")
	}
	converter := NewStreamConverter(plan)
	stream := collectStream(converter, []string{
		"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"先看\"}\n",
		"data: {\"type\":\"response.reasoning_text.delta\",\"delta\":\"目录\"}\n",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"好的\"}\n",
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n",
	})
	var reasoning, content strings.Builder
	for _, payload := range dataLines(stream) {
		delta := gjson.Get(payload, "choices.0.delta")
		reasoning.WriteString(delta.Get("reasoning_content").String())
		content.WriteString(delta.Get("content").String())
	}
	if actual := reasoning.String(); actual != "先看目录" {
		t.Fatalf("reasoning_content = %q, stream=%s", actual, stream)
	}
	if actual := content.String(); actual != "好的" {
		t.Fatalf("content = %q, stream=%s", actual, stream)
	}
}

// 客户端是 Responses、上游是 Chat（responses_to_chat）：上游 Chat 的
// reasoning_content 必须落成 Responses 的 reasoning 输出项。
func TestChatReasoningContentBecomesResponsesReasoning(t *testing.T) {
	plan, ok := fallbackPlan(ResponsesEndpoint)
	if !ok {
		t.Fatal("responses fallback plan missing")
	}
	converter := NewStreamConverter(plan)
	stream := collectStream(converter, []string{
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"想一想\"}}]}\n",
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"答案\"},\"finish_reason\":\"stop\"}]}\n",
		"data: [DONE]\n",
	})
	if !strings.Contains(stream, "response.reasoning_summary_text.delta") {
		t.Fatalf("missing reasoning delta event, stream=%s", stream)
	}
	var reasoningIndex, messageIndex int64 = -1, -1
	for _, payload := range dataLines(stream) {
		if gjson.Get(payload, "type").String() != "response.output_item.added" {
			continue
		}
		switch gjson.Get(payload, "item.type").String() {
		case "reasoning":
			reasoningIndex = gjson.Get(payload, "output_index").Int()
		case "message":
			messageIndex = gjson.Get(payload, "output_index").Int()
		}
	}
	// 下标不能相撞：reasoning 占 0，message 顺延到 1。
	if reasoningIndex != 0 || messageIndex != 1 {
		t.Fatalf("output_index collision: reasoning=%d message=%d, stream=%s", reasoningIndex, messageIndex, stream)
	}
	var completed string
	for _, payload := range dataLines(stream) {
		if gjson.Get(payload, "type").String() == "response.completed" {
			completed = payload
		}
	}
	if completed == "" {
		t.Fatalf("missing response.completed, stream=%s", stream)
	}
	if actual := gjson.Get(completed, "response.output.0.type").String(); actual != "reasoning" {
		t.Fatalf("first output item = %q, want reasoning, payload=%s", actual, completed)
	}
	if actual := gjson.Get(completed, "response.output.0.summary.0.text").String(); actual != "想一想" {
		t.Fatalf("reasoning summary = %q, payload=%s", actual, completed)
	}
	if actual := gjson.Get(completed, "response.output.1.type").String(); actual != "message" {
		t.Fatalf("second output item = %q, want message, payload=%s", actual, completed)
	}
}

// 没有思考内容时不得凭空申报 reasoning 项：非思考模型的行为保持原样。
func TestChatWithoutReasoningSkipsReasoningItem(t *testing.T) {
	plan, ok := fallbackPlan(ResponsesEndpoint)
	if !ok {
		t.Fatal("responses fallback plan missing")
	}
	converter := NewStreamConverter(plan)
	stream := collectStream(converter, []string{
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"gpt-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"答案\"},\"finish_reason\":\"stop\"}]}\n",
		"data: [DONE]\n",
	})
	if strings.Contains(stream, "\"reasoning\"") {
		t.Fatalf("no reasoning expected, stream=%s", stream)
	}
	for _, payload := range dataLines(stream) {
		if gjson.Get(payload, "type").String() == "response.output_item.added" {
			if index := gjson.Get(payload, "output_index").Int(); index != 0 {
				t.Fatalf("message must stay at output_index 0, payload=%s", payload)
			}
		}
	}
}

// 聚合方言 reasoning 与标准字段同样要转成 Responses 的思考项。
func TestChatAggregateReasoningDialectIsConverted(t *testing.T) {
	plan, ok := fallbackPlan(ResponsesEndpoint)
	if !ok {
		t.Fatal("responses fallback plan missing")
	}
	converter := NewStreamConverter(plan)
	stream := collectStream(converter, []string{
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"聚合方言\"},\"finish_reason\":\"stop\"}]}\n",
		"data: [DONE]\n",
	})
	if !strings.Contains(stream, "聚合方言") {
		t.Fatalf("aggregate reasoning lost, stream=%s", stream)
	}
}

// reasoning 项与工具调用的下标必须一起错开，否则客户端输出槽互相覆盖。
func TestChatReasoningShiftsToolOutputIndex(t *testing.T) {
	plan, ok := fallbackPlan(ResponsesEndpoint)
	if !ok {
		t.Fatal("responses fallback plan missing")
	}
	converter := NewStreamConverter(plan)
	stream := collectStream(converter, []string{
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"想一下\"}}]}\n",
		"data: {\"id\":\"chatcmpl-1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]}}]}\n",
		"data: [DONE]\n",
	})
	var reasoningIndex, messageIndex, toolIndex int64 = -1, -1, -1
	for _, payload := range dataLines(stream) {
		if gjson.Get(payload, "type").String() != "response.output_item.added" {
			continue
		}
		index := gjson.Get(payload, "output_index").Int()
		switch gjson.Get(payload, "item.type").String() {
		case "reasoning":
			reasoningIndex = index
		case "message":
			messageIndex = index
		case "function_call":
			toolIndex = index
		}
	}
	if reasoningIndex != 0 || messageIndex != 1 || toolIndex != 2 {
		t.Fatalf("indexes: reasoning=%d message=%d tool=%d, stream=%s", reasoningIndex, messageIndex, toolIndex, stream)
	}
}
