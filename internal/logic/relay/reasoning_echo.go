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

// DeepSeek、Kimi 等 thinking 模式的上游都是「无状态」的：只要本次对话发生过工具调用，
// 中间那条 assistant 消息的 reasoning_content 就必须在后续每一轮原样回传，否则上游
// 直接返回 HTTP 400。两家的提示分别是：
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
//     工具调用消息补回存档内容。
//
// 字段名不统一：直连厂商（DeepSeek/Kimi/GLM/MiMo）用 reasoning_content，OpenCode 系
// 聚合端点用 reasoning。存档时连字段名一起保存，回传沿用上游自己的方言，避免出现
// 「上游用 reasoning 返回、我们按 reasoning_content 补回去」而对方读不到的情况。
//
// 两条路径都只在「确实产生过思考内容」时生效，对不使用思考模式的模型完全没有副作用。
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

// patchReasoningContent 为「assistant + 带 tool_calls + 尚无思考内容字段」的消息补回思考内容。
// lookup 返回第一个命中的存档；Text 为空表示没有可补内容，消息保持原样。
// 写入的字段名取自存档（即上游返回时所用的方言）。
func patchReasoningContent(body []byte, lookup func(toolCallIDs []string) storedReasoning) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body
	}
	patched := body
	messages.ForEach(func(index, message gjson.Result) bool {
		if message.Get("role").String() != "assistant" {
			return true
		}
		// 客户端已按任一方言带上思考内容时不覆盖。
		if message.Get(reasoningFieldStandard).Exists() || message.Get(reasoningFieldCompact).Exists() {
			return true
		}
		toolCallIDs := chatToolCallIDs(message)
		if len(toolCallIDs) == 0 {
			return true
		}
		stored := lookup(toolCallIDs)
		if stored.Text == "" {
			return true
		}
		field := stored.Field
		if field == "" {
			field = reasoningFieldStandard
		}
		if next, err := sjson.SetBytes(patched, fmt.Sprintf("messages.%d.%s", index.Int(), field), stored.Text); err == nil {
			patched = next
		}
		return true
	})
	return patched
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
