package channel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// 会话密钥粘性：同一段连续对话固定使用同一把上游密钥。
//
// WHY：一把密钥就是一个上游账号。客户端在一段对话里反复换账号，上游看到的是
// 「同一条对话在多个账号之间跳」，比正常使用更像异常流量；用户明确要求
// 「新对话随机一把，后续继续对话不要换」。因此密钥亲和不再按键级（同一 API Key
// 的所有对话共用一把），而是会话级：首次挑选落记忆，之后按记忆复用。
//
// 与冷却/禁用的关系（决定「什么时候才换」）：
//   - 记忆里的密钥仍可用时永远优先，请求内的临时失败不覆盖记忆——下一次请求还会
//     回到这把，避免一次抖动就把整段对话永久换到另一把账号；
//   - 只有它真的不可用（状态禁用、渠道级冷却、模型×密钥组合冷却、被删除）时才改写
//     记忆。欠费/失效会被上游判成不可用并进入禁用或冷却，因此「没费用了自动换」
//     也走这条路径；
//   - 记忆是按 (会话, 渠道, 模型) 存的：换到备用渠道后，备用渠道上有自己的一把，
//     原渠道恢复时原渠道的那把还在，天然做到「恢复了就切回去」。
//
// 记忆放在 Redis，不建表：会话是短期状态，TTL 到期即回收，避免为转发热路径
// 增加数据库写与迁移。
const credentialAffinityTTL = 24 * time.Hour

// conversationAffinityContextKey 是 ctx 里携带会话标识的键。会话标识由转发层
// 解析（客户端会话头、metadata.user_id 或首条用户消息指纹），渠道层只消费。
type conversationAffinityContextKey struct{}

// WithConversationAffinity 把会话标识放进 ctx，SelectCredential 据此固定密钥。
// 空白标识直接返回原 ctx：没有会话标识的请求（管理端测试、单轮调用）保持原有
// 按键级绑定的行为，不引入新的随机性。
func WithConversationAffinity(ctx context.Context, conversationKey string) context.Context {
	trimmed := strings.TrimSpace(conversationKey)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, conversationAffinityContextKey{}, trimmed)
}

// conversationAffinityFromContext 读取 ctx 里的会话标识；没有时返回空串。
func conversationAffinityFromContext(ctx context.Context) string {
	value, _ := ctx.Value(conversationAffinityContextKey{}).(string)
	return strings.TrimSpace(value)
}

// credentialAffinityKey 是会话密钥记忆的 Redis 键。模型维度必须参与：同一段对话
// 可能在多个模型间切换，不同模型的密钥可用性不同，混在一起会让记忆互相踩。
func credentialAffinityKey(conversationKey string, channelID, modelID uint64) string {
	return fmt.Sprintf("aiferry:conversation-credential:%s:%d:%d", conversationKey, channelID, modelID)
}

// readCredentialAffinity 读取会话固定的密钥 ID；无记忆或读取失败视为 0（无记忆）。
// 命中时顺手续期：连续对话只要还在进行，记忆就不会因为 TTL 中途过期而换密钥。
func (s *sChannel) readCredentialAffinity(ctx context.Context, key string) uint64 {
	if s.app == nil || s.app.Redis == nil || key == "" {
		return 0
	}
	value, err := s.app.Redis.Get(ctx, key).Uint64()
	if err != nil || value == 0 {
		return 0
	}
	_ = s.app.Redis.Expire(ctx, key, credentialAffinityTTL).Err()
	return value
}

// writeCredentialAffinity 记录会话固定的密钥。写失败只影响下次是否会重新挑，
// 不影响本次转发，因此只记日志不外抛。
func (s *sChannel) writeCredentialAffinity(ctx context.Context, key string, credentialID uint64) {
	if s.app == nil || s.app.Redis == nil || key == "" || credentialID == 0 {
		return
	}
	if err := s.app.Redis.Set(ctx, key, credentialID, credentialAffinityTTL).Err(); err != nil {
		g.Log().Warningf(ctx, "记录会话密钥粘性失败 key=%s credential=%d: %v", key, credentialID, err)
	}
}

// selectConversationCredential 在「会话固定密钥」语义下挑选一把密钥。
//
//	available  本次可用密钥（已按渠道级冷却与模型×密钥组合冷却过滤，未应用本请求
//	           的排除集合），按 id 升序，保证无记忆时的加权随机可复现；
//	affinityID 该会话上次固定的密钥，0 表示还没有记忆；
//	excluded   本请求已经失败过的密钥，只在本次请求内生效。
//
// 返回 persist 表示是否应该把选中的密钥写回记忆。只有「记忆里的密钥真的不可用」
// 与「还没有记忆」两种情况才写：本请求内的临时排除（persist=false）不覆盖记忆，
// 这样一次抖动不会把整段对话永久换到另一把账号上。
func selectConversationCredential(available []credentialRow, affinityID uint64, excluded map[uint64]struct{}, comboHealth map[uint64]comboHealth) (credentialRow, bool) {
	usable := make([]credentialRow, 0, len(available))
	for _, row := range available {
		if _, skip := excluded[row.Id]; skip {
			continue
		}
		usable = append(usable, row)
	}
	if len(usable) == 0 {
		return credentialRow{}, false
	}
	if affinityID > 0 {
		for _, row := range available {
			if row.Id != affinityID {
				continue
			}
			// 记忆里的密钥仍可用：直接复用，除非本次请求已经把它排除。
			for _, candidate := range usable {
				if candidate.Id == affinityID {
					return candidate, false
				}
			}
			// 只是本次请求内的临时排除：本次换一把，但保留记忆。
			return weightedSelect(usable, comboHealth), false
		}
		// 记忆里的密钥已经不在可用池里（禁用/冷却/欠费/删除）：改记新的一把。
		return weightedSelect(usable, comboHealth), true
	}
	return weightedSelect(usable, comboHealth), true
}

// selectCredentialForConversation 是 selectConversationCredential 的取数外壳。
// 返回 false 表示「本请求没有会话标识，或会话路径下没有可用密钥」，由调用方
// 回到原有逻辑（那里会给出与过滤条件一致的不可用诊断文案）。
func (s *sChannel) selectCredentialForConversation(ctx context.Context, channelID, modelID uint64, excluded map[uint64]struct{}) (RouteCredential, bool) {
	conversationKey := conversationAffinityFromContext(ctx)
	if conversationKey == "" {
		return RouteCredential{}, false
	}
	rows, err := s.availableCredentials(ctx, channelID, nil)
	if err != nil {
		g.Log().Warningf(ctx, "读取会话密钥池失败 channel=%d: %v", channelID, err)
		return RouteCredential{}, false
	}
	comboHealth := s.comboHealthByCredential(ctx, channelID, modelID)
	available := make([]credentialRow, 0, len(rows))
	for _, row := range rows {
		if state, ok := comboHealth[row.Id]; ok && state.cooling {
			continue
		}
		available = append(available, row)
	}
	if len(available) == 0 {
		return RouteCredential{}, false
	}
	key := credentialAffinityKey(conversationKey, channelID, modelID)
	affinityID := s.readCredentialAffinity(ctx, key)
	selected, persist := selectConversationCredential(available, affinityID, excluded, comboHealth)
	if selected.Id == 0 {
		return RouteCredential{}, false
	}
	if persist && selected.Id != affinityID {
		s.writeCredentialAffinity(ctx, key, selected.Id)
		if affinityID > 0 {
			// 只在「原有记忆被替换」时留日志：新对话首次挑选不刷日志。
			g.Log().Infof(ctx, "会话密钥已切换 channel=%d model=%d credential=%d→%d（原密钥已不可用）",
				channelID, modelID, affinityID, selected.Id)
		}
	}
	return RouteCredential{ID: selected.Id, APIKeyCipher: selected.ApiKeyCipher}, true
}
