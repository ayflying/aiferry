package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

// 会话路由粘性：同一段连续对话固定走第一次选定的渠道优先顺序。
//
// WHY：候选顺序每个请求都重新做加权随机，因此两把权重接近的渠道会在一段对话里
// 来回跳；上游看到的是「同一条对话在多个账号/渠道之间跳」，而且一旦首选渠道抖动，
// 对昂贵备用渠道的使用会一直持续。用户要求：优先级与权重在**第一次对话时**选好，
// 后续对话不再换；渠道坏了才切备用；原渠道恢复后要切回去。
//
// 实现只记「优先顺序」，不记「当前在用的渠道」：
//   - 顺序里排在前面的渠道只要可用就一定先被选中；
//   - 坏掉的渠道（自动禁用/关闭时段/不可用）根本不在候选里，自然落到下一跳；
//   - 它恢复后重新回到候选，靠顺序自动回到第一位——不需要额外的「恢复检测」逻辑。
//
// 记忆按 (API Key, 模型, 会话标识) 存 Redis，TTL 到期回收。只存渠道 ID，
// 不含提示词内容与任何凭据。
const conversationAffinityTTL = 24 * time.Hour

// conversationAffinityRecord 是会话路由记忆的内容。只存渠道优先顺序：
// 渠道是否可以走由每次请求实时重算（禁用、关闭时段、分组策略、密钥可用性），
// 记忆只负责「谁优先」，不负责「谁可用」，避免记忆里留下过期可用性。
type conversationAffinityRecord struct {
	Channels []uint64 `json:"channels"`
}

func conversationAffinityRedisKey(apiKeyID uint64, model, conversationKey string) string {
	return fmt.Sprintf("aiferry:conversation-route:%d:%s:%s", apiKeyID, model, conversationKey)
}

// conversationKeyForRequest 解析本次请求所属的会话标识，返回值已做哈希，
// Redis 里不出现客户端原始会话串。解析顺序：
//
//  1. 客户端原生会话头（x-opencode-session / session_id / x-session-id / conversation_id…），
//     与上游客户端身份用的是同一套头，编码代理本来就会带；
//  2. 请求体的 metadata.user_id（Claude Code 一类客户端在其中带 session 标识）；
//  3. 首条用户消息的指纹：前两者都没有时，用「同一段对话的首条用户消息不变」这个
//     事实当锚点。多轮对话里首条消息不会变，因此同一段对话稳定命中同一记忆。
//
// 返回空串表示识别不出会话（既没有会话头，也取不到首条用户消息）：这类请求不做粘性，
// 保持原有的每请求随机，避免把互不相关的请求错并成一段对话。
func conversationKeyForRequest(incoming http.Header, body []byte, apiKeyID uint64) string {
	if session := channel.ClientSessionID(incoming); session != "" {
		return hashConversationKey("session|" + session)
	}
	if userID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String()); userID != "" {
		return hashConversationKey("metadata|" + userID)
	}
	first := firstUserMessageText(body)
	if first == "" {
		return ""
	}
	return hashConversationKey("first|" + strconv.FormatUint(apiKeyID, 10) + "|" + first)
}

// hashConversationKey 把会话标识压成定长键：原始会话串可能很长（客户端 UUID 串），
// 也可能带不可见字符，统一哈希后做 Redis 键更安全，也避免把提示词原文落到缓存键上。
func hashConversationKey(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:16])
}

// firstUserMessageText 取请求体里第一条用户消息的文本，兼容三种常见形状：
// chat/completions 的 messages[]、Responses 的 input[]（字符串或消息数组）、
// 以及 Anthropic 的 messages[]（内容为 parts 数组）。
func firstUserMessageText(body []byte) string {
	if text := firstUserMessageFrom(body, "messages"); text != "" {
		return text
	}
	input := gjson.GetBytes(body, "input")
	if input.Type == gjson.String {
		return strings.TrimSpace(input.String())
	}
	return firstUserMessageFrom(body, "input")
}

func firstUserMessageFrom(body []byte, field string) string {
	items := gjson.GetBytes(body, field)
	if !items.IsArray() {
		return ""
	}
	for _, item := range items.Array() {
		role := strings.TrimSpace(item.Get("role").String())
		// Responses 的 input 元素可能不带 role，只带 type=message + role 缺省为 user。
		if role == "" {
			if item.Get("type").String() != "message" {
				continue
			}
			role = "user"
		}
		if role != "user" {
			continue
		}
		if text := userMessageText(item.Get("content")); text != "" {
			return text
		}
	}
	return ""
}

func userMessageText(content gjson.Result) string {
	if content.Type == gjson.String {
		return strings.TrimSpace(content.String())
	}
	if !content.IsArray() {
		return ""
	}
	parts := make([]string, 0, len(content.Array()))
	for _, part := range content.Array() {
		if text := strings.TrimSpace(part.Get("text").String()); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// applyConversationChannelAffinity 按会话记忆的优先顺序重排候选：
// 记忆里排前的渠道先，其余候选保持本次的加权顺序接在后面。
// 记忆里已不可用（被禁用、过滤、删除）的渠道直接跳过——这就是「坏了切备用」，
// 它恢复后重新出现在候选里又会排到前面，即「恢复了切回去」。
func applyConversationChannelAffinity(candidates []Candidate, pinned []uint64) []Candidate {
	if len(pinned) == 0 || len(candidates) == 0 {
		return candidates
	}
	pending := make(map[uint64][]Candidate, len(candidates))
	for _, candidate := range candidates {
		pending[candidate.ChannelID] = append(pending[candidate.ChannelID], candidate)
	}
	ordered := make([]Candidate, 0, len(candidates))
	for _, channelID := range pinned {
		queue := pending[channelID]
		if len(queue) == 0 {
			continue
		}
		ordered = append(ordered, queue[0])
		pending[channelID] = queue[1:]
	}
	for _, candidate := range candidates {
		queue := pending[candidate.ChannelID]
		if len(queue) == 0 {
			continue
		}
		ordered = append(ordered, queue[0])
		pending[candidate.ChannelID] = queue[1:]
	}
	return ordered
}

// candidateChannelIDs 按当前顺序取出渠道 ID，用于第一次对话时落记忆。
func candidateChannelIDs(candidates []Candidate) []uint64 {
	ids := make([]uint64, 0, len(candidates))
	seen := make(map[uint64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, exists := seen[candidate.ChannelID]; exists {
			continue
		}
		seen[candidate.ChannelID] = struct{}{}
		ids = append(ids, candidate.ChannelID)
	}
	return ids
}

// readConversationChannelAffinity 读取会话记忆的渠道顺序；无记忆或读取失败返回 nil。
// 命中时顺手续期，连续对话不会因为 TTL 中途到期而重新洗牌。
func (s *sRelay) readConversationChannelAffinity(ctx context.Context, key string) []uint64 {
	if s.app == nil || s.app.Redis == nil || key == "" {
		return nil
	}
	encoded, err := s.app.Redis.Get(ctx, key).Bytes()
	if err != nil || len(encoded) == 0 {
		return nil
	}
	var record conversationAffinityRecord
	if err := json.Unmarshal(encoded, &record); err != nil || len(record.Channels) == 0 {
		_ = s.app.Redis.Del(ctx, key).Err()
		return nil
	}
	_ = s.app.Redis.Expire(ctx, key, conversationAffinityTTL).Err()
	return record.Channels
}

// writeConversationChannelAffinity 记录会话的渠道优先顺序。写失败只影响下一次
// 是否会重新洗牌，不影响本次转发，因此不外抛。
func (s *sRelay) writeConversationChannelAffinity(ctx context.Context, key string, channels []uint64) {
	if s.app == nil || s.app.Redis == nil || key == "" || len(channels) == 0 {
		return
	}
	encoded, err := json.Marshal(conversationAffinityRecord{Channels: channels})
	if err != nil {
		return
	}
	_ = s.app.Redis.Set(ctx, key, encoded, conversationAffinityTTL).Err()
}
