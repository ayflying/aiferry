package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/yunloli/aiferry/internal/logic/protocol"
)

// DeepSeek、Kimi 等 thinking 模式的上游都是「无状态」的：发生过工具调用的当前轮里，
// assistant 消息的 reasoning_content 必须原样回传，否则上游直接返回 HTTP 400。两家的
// 提示分别是：
//
//	DeepSeek: The `reasoning_content` in the thinking mode must be passed back to the API.
//	Kimi:     thinking is enabled but reasoning_content is missing in assistant tool call message at index N
//
// 该约束只与「具体模型是否开启了思考模式」有关，与渠道类型无关：同一个渠道（例如
// opencode_go 这类聚合渠道）下既有 deepseek/kimi 这类思考模型，也有 grok/gpt 这类
// 不需要回传的模型，因此这里不做渠道类型或模型名白名单判断。
//
// reasoning_content 并不是 OpenAI Chat Completions 的标准字段，绝大多数兼容客户端
// 重建历史消息时只保留 role/content/tool_calls，会把它丢掉；网关原样透传也无法补救。
// 转发层因此承担两件事：
//  1. rememberReasoningContent：把上游返回的思考内容按 tool_call id 存档；
//  2. restoreReasoningContent：客户端下一轮回传历史时，为缺少该字段的 assistant
//     工具调用消息补回存档内容；存档与客户端都没有内容时，为「当前轮」的消息补空串。
//
// 字段名不统一：直连厂商（DeepSeek/Kimi/GLM/MiMo）用 reasoning_content，OpenCode 系
// 聚合端点用 reasoning。capture 侧两种名字都识别（存档时把命中的名字一并保存，仅用于
// 诊断与兼容历史存档）；但 patch 侧统一只写 reasoning_content 并删除 reasoning，理由见
// patchReasoningContent 的字段容忍度实测表——reasoning 是「有毒字段」。
//
// 补空串只发生在「最后一条 user 消息之后」的工具调用消息上（上游只校验这一段，见
// patchReasoningContent 的边界实测），对不使用思考模式的模型与已翻篇的历史消息都没有副作用。
const (
	// reasoningEchoTTL 需要覆盖一次多步工具调用链的持续时间，超时后回退为不补（客户端会拿到上游的原始 400）。
	reasoningEchoTTL = 24 * time.Hour
	// reasoningEchoKeyPrefix 独立命名空间，便于排查与清理。
	reasoningEchoKeyPrefix = "aiferry:reasoning:"
	// maxReasoningEchoBytes 单条思考内容的存档上限，避免异常上游写出超大键值。
	maxReasoningEchoBytes = 1 << 20

	// reasoningFieldStandard 是 DeepSeek / Kimi / GLM 等厂商返回思考内容所用的字段名。
	reasoningFieldStandard = "reasoning_content"
	// reasoningFieldCompact 是 OpenCode 系聚合端点（含 opencode_go）所用的字段名，
	// 语义与 reasoningFieldStandard 相同，只是名字更短。
	reasoningFieldCompact = "reasoning"
)

// reasoningFromContainer 按优先级从 message/delta 容器里读取思考内容，并回报实际命中的
// 字段名，供回传时沿用。返回空串表示该容器没有思考内容。
func reasoningFromContainer(container gjson.Result) (string, string) {
	if text := container.Get(reasoningFieldStandard).String(); text != "" {
		return text, reasoningFieldStandard
	}
	if text := container.Get(reasoningFieldCompact).String(); text != "" {
		return text, reasoningFieldCompact
	}
	return "", ""
}

// reasoningCapture 从上游 Chat Completions 响应里累积思考内容与工具调用 id。
// 流式场景必须在响应改写（normalizeSSELine）之前观察：渠道开启 ReasoningToContent 时，
// 改写会把 reasoning_content 合并进 content 并删除原字段。
type reasoningCapture struct {
	endpoint  string
	reasoning strings.Builder
	field     string
	toolCalls []string
	seen      map[string]struct{}
}

func newReasoningCapture(endpoint string) *reasoningCapture {
	return &reasoningCapture{endpoint: endpoint, seen: make(map[string]struct{})}
}

// Observe 处理一行上游 SSE。先做子串预筛，避免为无关行解析 JSON。
func (c *reasoningCapture) Observe(line []byte) {
	if c == nil || c.endpoint != protocol.ChatCompletionsEndpoint {
		return
	}
	// "reasoning" 是 "reasoning_content" 的子串，一个判断即可覆盖两种方言。
	if !bytes.Contains(line, []byte(reasoningFieldCompact)) && !bytes.Contains(line, []byte("tool_calls")) {
		return
	}
	payload, done, valid := relaySSEDataPayload(line)
	if !valid || done {
		return
	}
	delta := gjson.GetBytes(payload, "choices.0.delta")
	if text, field := reasoningFromContainer(delta); text != "" {
		if c.field == "" {
			c.field = field
		}
		c.reasoning.WriteString(text)
	}
	for _, value := range delta.Get("tool_calls").Array() {
		c.addToolCall(value.Get("id").String())
	}
}

func (c *reasoningCapture) Reasoning() string {
	if c == nil {
		return ""
	}
	return c.reasoning.String()
}

// Field 返回上游实际使用的思考内容字段名，未捕获到时按标准字段名处理。
func (c *reasoningCapture) Field() string {
	if c == nil || c.field == "" {
		return reasoningFieldStandard
	}
	return c.field
}

func (c *reasoningCapture) ToolCallIDs() []string {
	if c == nil {
		return nil
	}
	return c.toolCalls
}

func (c *reasoningCapture) addToolCall(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if _, exists := c.seen[id]; exists {
		return
	}
	c.seen[id] = struct{}{}
	c.toolCalls = append(c.toolCalls, id)
}

// captureBufferedReasoning 读取非流式 Chat Completions 响应里的思考内容（含字段名）与工具调用 id。
// 与流式一样，必须在 normalizeResponseBody 之前调用。
func captureBufferedReasoning(endpoint string, body []byte) (string, string, []string) {
	if endpoint != protocol.ChatCompletionsEndpoint {
		return "", "", nil
	}
	message := gjson.GetBytes(body, "choices.0.message")
	if !message.Exists() {
		return "", "", nil
	}
	ids := make([]string, 0, 1)
	for _, value := range message.Get("tool_calls").Array() {
		if id := strings.TrimSpace(value.Get("id").String()); id != "" {
			ids = append(ids, id)
		}
	}
	text, field := reasoningFromContainer(message)
	return text, field, ids
}

func reasoningEchoKey(apiKeyID uint64, toolCallID string) string {
	return fmt.Sprintf("%s%d:%s", reasoningEchoKeyPrefix, apiKeyID, toolCallID)
}

// storedReasoning 是存档结构：字段名随内容一起保存，回传时沿用上游方言。
type storedReasoning struct {
	Field string `json:"field"`
	Text  string `json:"text"`
}

func encodeStoredReasoning(field, text string) string {
	if field == "" {
		field = reasoningFieldStandard
	}
	payload, err := json.Marshal(storedReasoning{Field: field, Text: text})
	if err != nil {
		return text
	}
	return string(payload)
}

// decodeStoredReasoning 解析存档；非 JSON 的历史存档按纯文本、标准字段名兼容处理。
func decodeStoredReasoning(value string) storedReasoning {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "{") {
		return storedReasoning{Field: reasoningFieldStandard, Text: value}
	}
	var stored storedReasoning
	if err := json.Unmarshal([]byte(trimmed), &stored); err != nil || stored.Text == "" {
		return storedReasoning{}
	}
	if stored.Field == "" {
		stored.Field = reasoningFieldStandard
	}
	return stored
}

// rememberReasoningContent 把本轮上游返回的思考内容按 tool_call id 存档。
// 客户端下一轮带上同一个 tool_call id 时即可补回，密钥作用域避免不同用户之间串号。
func (s *sRelay) rememberReasoningContent(ctx context.Context, apiKeyID uint64, result attemptResult) {
	if apiKeyID == 0 || s.app == nil || s.app.Redis == nil {
		return
	}
	reasoning := result.reasoningContent
	if reasoning == "" || len(result.reasoningToolCallIDs) == 0 {
		return
	}
	if len(reasoning) > maxReasoningEchoBytes {
		g.Log().Warningf(ctx, "skip storing reasoning content api_key=%d bytes=%d exceeds limit", apiKeyID, len(reasoning))
		return
	}
	payload := encodeStoredReasoning(result.reasoningField, reasoning)
	pipeline := s.app.Redis.Pipeline()
	for _, id := range result.reasoningToolCallIDs {
		pipeline.Set(ctx, reasoningEchoKey(apiKeyID, id), payload, reasoningEchoTTL)
	}
	if _, err := pipeline.Exec(ctx); err != nil {
		g.Log().Warningf(ctx, "store reasoning content api_key=%d: %v", apiKeyID, err)
	}
}

// restoreReasoningContent 为客户端重建的历史消息补回思考内容。
// 没有存档时保持原样，由上游按自己的规则处理。
func (s *sRelay) restoreReasoningContent(ctx context.Context, body []byte, apiKeyID uint64) []byte {
	if apiKeyID == 0 || s.app == nil || s.app.Redis == nil {
		return body
	}
	return patchReasoningContent(body, func(toolCallIDs []string) storedReasoning {
		keys := make([]string, 0, len(toolCallIDs))
		for _, id := range toolCallIDs {
			keys = append(keys, reasoningEchoKey(apiKeyID, id))
		}
		values, err := s.app.Redis.MGet(ctx, keys...).Result()
		if err != nil {
			g.Log().Warningf(ctx, "restore reasoning content api_key=%d: %v", apiKeyID, err)
			return storedReasoning{}
		}
		for _, value := range values {
			text, ok := value.(string)
			if !ok || text == "" {
				continue
			}
			if stored := decodeStoredReasoning(text); stored.Text != "" {
				return stored
			}
		}
		return storedReasoning{}
	})
}

// patchReasoningContent 为「assistant + 带 tool_calls」的消息规整思考内容字段。
//
// 字段名统一为 reasoning_content，实测这是唯一各方都接受的名字（OpenCode Go / Zen
// 全 37 模型实测）：
//
//	字段取值                        GLM 系   DeepSeek 系   Kimi 系
//	reasoning_content = ""          200      200           200
//	reasoning_content = null        200      400           200
//	reasoning_content 缺失          200      400           200
//	reasoning = 任意值（含 ""/null） 400      400           200
//
// 由此得到两条必须遵守的规则：
//
//  1. reasoning 是「有毒字段」：GLM 与 DeepSeek 看到它就 400，哪怕只是空串或 null。
//     客户端（部分 Codex 系客户端按聚合方言重建历史）可能带上它，必须删除；其内容若为
//     唯一来源，则转写到 reasoning_content。
//  2. reasoning_content 必须存在且为字符串：null 与「字段缺失」都会被 DeepSeek 判为
//     未回传而 400（gjson 的 Exists() 对 JSON null 返回 true，所以不能拿它当「已带」）。
//
// 第 2 条只在「最后一条 user 消息之后」的消息上被上游校验——这是实测出来的边界：
//
//	[user, assistant(tool_calls, 无 rc), tool]                       -> 400  当前轮缺字段
//	[user, assistant(tool_calls, 无 rc), tool, user]                 -> 200  已被 user 翻篇
//	[user, assistant(tool_calls, 无 rc), tool, user, assistant(..)]  -> 400  又进入当前轮
//	[user, assistant(tool_calls, 无 rc), tool, assistant("done")]    -> 400  当前轮
//	[user, assistant(tool_calls, rc=""), tool]                       -> 200  空串可接受
//
// 也就是说上一轮工具调用的 assistant 消息一旦被后续 user 消息「翻篇」就不再参与校验。
// 因此只需要为「最后一条 user 消息之后」的工具调用消息兜底：取不到任何思考内容时补一个
// 空串，既满足上游，又不会给已经翻篇的历史消息平白加字段。
func patchReasoningContent(body []byte, lookup func(toolCallIDs []string) storedReasoning) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body
	}
	// 上游只校验最后一条 user 消息之后的「当前轮」消息。
	lastUserIndex := int64(-1)
	messages.ForEach(func(index, message gjson.Result) bool {
		if message.Get("role").String() == "user" {
			lastUserIndex = index.Int()
		}
		return true
	})
	patched := body
	messages.ForEach(func(index, message gjson.Result) bool {
		if message.Get("role").String() != "assistant" {
			return true
		}
		toolCallIDs := chatToolCallIDs(message)
		if len(toolCallIDs) == 0 {
			return true
		}
		// 客户端可能按聚合方言带了内容，先取出来（优先标准名），再统一规整。
		clientText := strings.TrimSpace(message.Get(reasoningFieldStandard).String())
		if clientText == "" {
			clientText = strings.TrimSpace(message.Get(reasoningFieldCompact).String())
		}
		// reasoning 是有毒字段，任何取值都要删掉。
		patched = deleteReasoningField(patched, index.Int(), reasoningFieldCompact)
		if clientText != "" {
			// 客户端确实带过思考内容：统一落到标准字段。
			patched = setReasoningField(patched, index.Int(), reasoningFieldStandard, clientText)
			return true
		}
		if stored := lookup(toolCallIDs); stored.Text != "" {
			patched = setReasoningField(patched, index.Int(), reasoningFieldStandard, stored.Text)
			return true
		}
		// 兜底：当前轮的工具调用消息必须带字段，取不到内容时补空串。
		if index.Int() > lastUserIndex {
			patched = setReasoningField(patched, index.Int(), reasoningFieldStandard, "")
			return true
		}
		// 已翻篇的历史消息：只在客户端显式写了 null 时归一成空串。
		if value := message.Get(reasoningFieldStandard); value.Exists() && value.Type == gjson.Null {
			patched = setReasoningField(patched, index.Int(), reasoningFieldStandard, "")
		}
		return true
	})
	return patched
}

func setReasoningField(body []byte, index int64, field, text string) []byte {
	next, err := sjson.SetBytes(body, fmt.Sprintf("messages.%d.%s", index, field), text)
	if err != nil {
		return body
	}
	return next
}

func deleteReasoningField(body []byte, index int64, field string) []byte {
	next, err := sjson.DeleteBytes(body, fmt.Sprintf("messages.%d.%s", index, field))
	if err != nil {
		return body
	}
	return next
}

func chatToolCallIDs(message gjson.Result) []string {
	calls := message.Get("tool_calls")
	if !calls.IsArray() {
		return nil
	}
	ids := make([]string, 0, len(calls.Array()))
	for _, call := range calls.Array() {
		if id := strings.TrimSpace(call.Get("id").String()); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
