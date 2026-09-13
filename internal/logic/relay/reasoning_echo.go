package relay

import (
	"bytes"
	"context"
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
// 两条路径都只在「确实产生过思考内容」时生效，对不使用思考模式的模型完全没有副作用。
const (
	// reasoningEchoTTL 需要覆盖一次多步工具调用链的持续时间，超时后回退为不补（客户端会拿到上游的原始 400）。
	reasoningEchoTTL = 24 * time.Hour
	// reasoningEchoKeyPrefix 独立命名空间，便于排查与清理。
	reasoningEchoKeyPrefix = "aiferry:reasoning:"
	// maxReasoningEchoBytes 单条思考内容的存档上限，避免异常上游写出超大键值。
	maxReasoningEchoBytes = 1 << 20
)

// reasoningCapture 从上游 Chat Completions 响应里累积思考内容与工具调用 id。
// 流式场景必须在响应改写（normalizeSSELine）之前观察：渠道开启 ReasoningToContent 时，
// 改写会把 reasoning_content 合并进 content 并删除原字段。
type reasoningCapture struct {
	endpoint  string
	reasoning strings.Builder
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
	if !bytes.Contains(line, []byte("reasoning_content")) && !bytes.Contains(line, []byte("tool_calls")) {
		return
	}
	payload, done, valid := relaySSEDataPayload(line)
	if !valid || done {
		return
	}
	if delta := gjson.GetBytes(payload, "choices.0.delta.reasoning_content").String(); delta != "" {
		c.reasoning.WriteString(delta)
	}
	for _, value := range gjson.GetBytes(payload, "choices.0.delta.tool_calls").Array() {
		c.addToolCall(value.Get("id").String())
	}
}

func (c *reasoningCapture) Reasoning() string {
	if c == nil {
		return ""
	}
	return c.reasoning.String()
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

// captureBufferedReasoning 读取非流式 Chat Completions 响应里的思考内容与工具调用 id。
// 与流式一样，必须在 normalizeResponseBody 之前调用。
func captureBufferedReasoning(endpoint string, body []byte) (string, []string) {
	if endpoint != protocol.ChatCompletionsEndpoint {
		return "", nil
	}
	message := gjson.GetBytes(body, "choices.0.message")
	if !message.Exists() {
		return "", nil
	}
	ids := make([]string, 0, 1)
	for _, value := range message.Get("tool_calls").Array() {
		if id := strings.TrimSpace(value.Get("id").String()); id != "" {
			ids = append(ids, id)
		}
	}
	return message.Get("reasoning_content").String(), ids
}

func reasoningEchoKey(apiKeyID uint64, toolCallID string) string {
	return fmt.Sprintf("%s%d:%s", reasoningEchoKeyPrefix, apiKeyID, toolCallID)
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
	pipeline := s.app.Redis.Pipeline()
	for _, id := range result.reasoningToolCallIDs {
		pipeline.Set(ctx, reasoningEchoKey(apiKeyID, id), reasoning, reasoningEchoTTL)
	}
	if _, err := pipeline.Exec(ctx); err != nil {
		g.Log().Warningf(ctx, "store reasoning content api_key=%d: %v", apiKeyID, err)
	}
}

// restoreReasoningContent 为客户端重建的历史消息补回 reasoning_content。
// 没有存档时保持原样，由上游按自己的规则处理。
func (s *sRelay) restoreReasoningContent(ctx context.Context, body []byte, apiKeyID uint64) []byte {
	if apiKeyID == 0 || s.app == nil || s.app.Redis == nil {
		return body
	}
	return patchReasoningContent(body, func(toolCallIDs []string) string {
		keys := make([]string, 0, len(toolCallIDs))
		for _, id := range toolCallIDs {
			keys = append(keys, reasoningEchoKey(apiKeyID, id))
		}
		values, err := s.app.Redis.MGet(ctx, keys...).Result()
		if err != nil {
			g.Log().Warningf(ctx, "restore reasoning content api_key=%d: %v", apiKeyID, err)
			return ""
		}
		for _, value := range values {
			if text, ok := value.(string); ok && text != "" {
				return text
			}
		}
		return ""
	})
}

// patchReasoningContent 为「assistant + 带 tool_calls + 缺 reasoning_content」的消息补回思考内容。
// lookup 返回第一个命中的存档；返回空串表示没有可补内容，消息保持原样。
func patchReasoningContent(body []byte, lookup func(toolCallIDs []string) string) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body
	}
	patched := body
	messages.ForEach(func(index, message gjson.Result) bool {
		if message.Get("role").String() != "assistant" || message.Get("reasoning_content").Exists() {
			return true
		}
		toolCallIDs := chatToolCallIDs(message)
		if len(toolCallIDs) == 0 {
			return true
		}
		text := lookup(toolCallIDs)
		if text == "" {
			return true
		}
		if next, err := sjson.SetBytes(patched, fmt.Sprintf("messages.%d.reasoning_content", index.Int()), text); err == nil {
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
