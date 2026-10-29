package protocol

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

// 工具调用在两条 chat↔responses 流式转换方向上的收口逻辑。实现参考：
//   - new-api ResponsesToChatStreamState（relaykit/relayconvert/internal/oai_responses）：
//     每条调用维护「累计参数 + 已发送水位线」，任何事件（output_item.added、
//     function_call_arguments.delta/done）更新累计参数后只补发未下发的尾部，
//     天然幂等；收尾时 flushAllPendingTools 兜底，参数只出现在 done 里时不再丢失。
//   - sub2api apicompat ChatCompletionsToResponsesStreamState
//     （backend/internal/pkg/apicompat/chatcompletions_responses_bridge.go）：
//     Responses 侧 output_index 必须独立顺序分配（消息项占 0，工具项从 1 起），
//     item_id/call_id 在申报时固定、后续分片全量携带；名称未到达前缓冲参数；
//     收尾必须逐调用补 function_call_arguments.done + output_item.done，否则
//     Codex 类客户端不会把调用落定。
//
// 生产事故（2026-10-15，DSH gpt-6.1-sol 团队工具调用连续报 missing required
// property）：部分 Responses 上游把完整参数只放在 output_item.added 的
// item.arguments 里、不随 delta 流式下发；早期 responsesToChat 只转发工具名，
// 客户端拿到空参数，schema 校验必然失败。

// responsesToolState 记录一次「客户端 Responses ← 上游 Chat」的工具调用。
type responsesToolState struct {
	upstreamIndex int
	outputIndex   int
	itemID        string
	callID        string
	name          string
	args          strings.Builder
	sentArgs      int
	announced     bool
}

// finalizedArgs 返回收尾用的参数文本；空缺时以 "{}" 收口，避免客户端解析残缺 JSON。
func (t *responsesToolState) finalizedArgs() string {
	args := t.args.String()
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

// responsesToolFor 以上游 Chat delta.tool_calls 的 index 为身份返回该调用的状态。
// Responses 侧的 output_index 不能照抄上游 chat 下标，更不能与消息项共用 0：
// 并行调用必须各自占用独立槽位，否则客户端的输出槽互相覆盖（sub2api 的教训）。
func (c *StreamConverter) responsesToolFor(value gjson.Result) *responsesToolState {
	upstreamIndex := int(value.Get("index").Int())
	if tool, exists := c.responsesTools[upstreamIndex]; exists {
		return tool
	}
	// 消息项占用 responsesMessageIndex()（reasoning 已申报时为 1，否则 0），
	// 工具项紧随其后连续分配。
	next := c.responsesMessageIndex() + 1 + c.responsesToolSeq
	tool := &responsesToolState{
		upstreamIndex: upstreamIndex,
		outputIndex:   next,
		itemID:        fmt.Sprintf("fc_aiferry_%d", next),
		callID:        fmt.Sprintf("call_aiferry_%d", next),
	}
	c.responsesToolSeq++
	c.responsesTools[upstreamIndex] = tool
	return tool
}

// sortedResponsesTools 按 Responses 槽位顺序返回全部工具调用状态。
func (c *StreamConverter) sortedResponsesTools() []*responsesToolState {
	tools := make([]*responsesToolState, 0, len(c.responsesTools))
	for _, tool := range c.responsesTools {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].outputIndex < tools[j].outputIndex })
	return tools
}

// responsesToolArgsDelta 生成一条参数增量事件。身份字段（output_index、item_id、
// call_id）全量携带——上游 Chat 的参数分片往往只有首片带 id，不能把 item_id 留空。
func (c *StreamConverter) responsesToolArgsDelta(tool *responsesToolState, delta string) []byte {
	return sseEvent("response.function_call_arguments.delta", map[string]any{
		"type":         "response.function_call_arguments.delta",
		"output_index": tool.outputIndex,
		"item_id":      tool.itemID,
		"call_id":      tool.callID,
		"delta":        delta,
	})
}

// responsesToolPendingArgs 返回该调用已累计但尚未下发的参数尾部，并推进水位线。
func responsesToolPendingArgs(tool *responsesToolState) string {
	args := tool.args.String()
	if len(args) <= tool.sentArgs {
		return ""
	}
	tail := args[tool.sentArgs:]
	tool.sentArgs = len(args)
	return tail
}

// responsesToolAnnounceItem 生成该调用的 output_item.added 申报事件。
func responsesToolAnnounceItem(tool *responsesToolState) []byte {
	return sseEvent("response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": tool.outputIndex,
		"item": map[string]any{
			"id": tool.itemID, "type": "function_call", "status": "in_progress",
			"call_id": tool.callID, "name": tool.name, "arguments": "",
		},
	})
}

// chatToolAppendArgs 把一条流式参数分片追加进累计值，并返回尚未转发给客户端
// 的尾部（水位线语义）。正常流式下尾部恰好等于本次分片；分片重复或乱序时，
// 已下发过的内容不会重复转发。
func (c *StreamConverter) chatToolAppendArgs(index int, fragment string) string {
	if fragment == "" {
		return ""
	}
	builder := c.chatToolArgs[index]
	if builder == nil {
		builder = &strings.Builder{}
		c.chatToolArgs[index] = builder
	}
	builder.WriteString(fragment)
	sent := c.chatToolArgsSent[index]
	args := builder.String()
	if len(args) <= sent {
		return ""
	}
	tail := args[sent:]
	c.chatToolArgsSent[index] = len(args)
	return tail
}

// chatToolRecordArgs 把一次调用的权威参数全量收进累计值，并返回尚未转发给
// 客户端的尾部（水位线语义）。全量必须是已下发内容的延长（前缀关系）才被
// 采纳；added/done 重复、或与已流式下发的分片冲突时，已下发内容优先，
// 不会重复转发。供 output_item.added 与 function_call_arguments.done 使用。
func (c *StreamConverter) chatToolRecordArgs(index int, canonical string) string {
	if canonical == "" {
		return ""
	}
	builder := c.chatToolArgs[index]
	if builder == nil {
		builder = &strings.Builder{}
		c.chatToolArgs[index] = builder
	}
	if accumulated := builder.String(); strings.HasPrefix(canonical, accumulated) {
		builder.Reset()
		builder.WriteString(canonical)
	}
	sent := c.chatToolArgsSent[index]
	args := builder.String()
	if len(args) <= sent {
		return ""
	}
	tail := args[sent:]
	c.chatToolArgsSent[index] = len(args)
	return tail
}

// chatToolArgsChunk 生成一条工具参数增量分片；id 仅在已知时携带。
func (c *StreamConverter) chatToolArgsChunk(index int, id, args string) [][]byte {
	call := map[string]any{"index": index, "type": "function", "function": map[string]any{"arguments": args}}
	if id != "" {
		call["id"] = id
	}
	return c.chatChunk(map[string]any{"tool_calls": []any{call}}, nil, nil)
}

// chatToolFlushPendingArgs 在收尾前把所有调用尚未下发的参数尾部补发出去。
// 这是参数只出现在 function_call_arguments.done（或整条流只有一个分片）时的
// 最后兜底，对应 new-api 的 flushAllPendingTools。
func (c *StreamConverter) chatToolFlushPendingArgs() [][]byte {
	indexes := make([]int, 0, len(c.chatToolArgs))
	for index := range c.chatToolArgs {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	var result [][]byte
	for _, index := range indexes {
		args := c.chatToolArgs[index].String()
		sent := c.chatToolArgsSent[index]
		if len(args) <= sent {
			continue
		}
		c.chatToolArgsSent[index] = len(args)
		c.sawToolCall = true
		result = append(result, c.chatToolArgsChunk(index, "", args[sent:])...)
	}
	return result
}
