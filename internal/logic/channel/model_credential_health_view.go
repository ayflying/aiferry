package channel

import (
	"context"
	"regexp"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// modelCredentialHealthRow 是 channel_model_credentials 表中与「模型 × 密钥」组合相关的只读字段。
// 仅选取展示所需的列，绝不映射 api_key_cipher / key_hash / management_key_cipher 等密钥密文/明文。
type modelCredentialHealthRow struct {
	ChannelModelId      uint64      `orm:"channel_model_id"`
	ChannelCredentialId uint64      `orm:"channel_credential_id"`
	HealthScore         int         `orm:"health_score"`
	CooldownUntil       *gtime.Time `orm:"cooldown_until"`
	LastError           string      `orm:"last_error"`
}

// validCredentialRow 是渠道下「有效」推理密钥的只读展示字段，仅含 id / 渠道 / 前缀。
type validCredentialRow struct {
	Id        uint64 `orm:"id"`
	ChannelId uint64 `orm:"channel_id"`
	KeyPrefix string `orm:"key_prefix"`
}

// loadModelCredentialHealth 批量加载模型关联的「密钥组合健康」只读视图。
//
// 口径：
//   - 仅纳入「有效（status=1）且未软删」的渠道密钥；dao 的 Where 已自动过滤软删。
//   - channel_model_credentials 没有软删字段，直接按 channel_model_id 批量取出组合行。
//   - 没有组合行的有效密钥默认 100 分（视为健康、未记录异常）。
//   - last_error 在只读路径上统一脱敏，不信任写入源，避免泄露上游密钥。
//
// 返回 map 以 channel_models.id 为键；若该模型所属渠道没有有效密钥，则不写入该键。
// 本函数只做读取，不写入任何健康分或隔离状态。
func (s *sChannel) loadModelCredentialHealth(ctx context.Context, models []entity.ChannelModels) (map[uint64][]*CredentialHealthView, error) {
	result := make(map[uint64][]*CredentialHealthView, len(models))
	if len(models) == 0 {
		return result, nil
	}

	// 收集涉及的渠道与模型 ID。
	channelIDs := make([]uint64, 0, len(models))
	seenChannel := make(map[uint64]struct{}, len(models))
	modelIDs := make([]uint64, 0, len(models))
	for _, model := range models {
		modelIDs = append(modelIDs, model.Id)
		if _, ok := seenChannel[model.ChannelId]; !ok {
			seenChannel[model.ChannelId] = struct{}{}
			channelIDs = append(channelIDs, model.ChannelId)
		}
	}

	// 1) 拉取这些渠道下所有「有效」密钥（已自动过滤软删），仅取展示所需字段。
	credCols := dao.ChannelCredentials.Columns()
	validCreds := make([]validCredentialRow, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).
		Fields(credCols.Id, credCols.ChannelId, credCols.KeyPrefix).
		WhereIn(credCols.ChannelId, channelIDs).
		Where(credCols.Status, 1).
		OrderAsc(credCols.Id).
		Scan(&validCreds); err != nil {
		return nil, gerror.Wrap(err, "load channel credentials for model health")
	}
	// 按渠道分组有效密钥，便于后续为每个模型复用。
	credsByChannel := make(map[uint64][]validCredentialRow, len(channelIDs))
	for _, cred := range validCreds {
		credsByChannel[cred.ChannelId] = append(credsByChannel[cred.ChannelId], cred)
	}

	// 2) 批量拉取这些模型的组合健康行。
	cmcCols := dao.ChannelModelCredentials.Columns()
	combos := make([]modelCredentialHealthRow, 0)
	if err := dao.ChannelModelCredentials.Ctx(ctx).
		Fields(cmcCols.ChannelModelId, cmcCols.ChannelCredentialId, cmcCols.HealthScore, cmcCols.CooldownUntil, cmcCols.LastError).
		WhereIn(cmcCols.ChannelModelId, modelIDs).
		Scan(&combos); err != nil {
		return nil, gerror.Wrap(err, "load model credential health")
	}
	// 以 (channelModelId -> channelCredentialId) 索引组合行，便于 O(1) 覆盖默认分。
	comboByModelCred := make(map[uint64]map[uint64]modelCredentialHealthRow, len(modelIDs))
	for _, combo := range combos {
		if _, ok := comboByModelCred[combo.ChannelModelId]; !ok {
			comboByModelCred[combo.ChannelModelId] = make(map[uint64]modelCredentialHealthRow)
		}
		comboByModelCred[combo.ChannelModelId][combo.ChannelCredentialId] = combo
	}

	// 3) 为每个模型构建健康视图：默认纳入该渠道全部有效密钥（默认 100），
	//    再用存在的组合行覆盖分数、隔离到期与错误摘要（错误摘要在此脱敏）。
	for _, model := range models {
		creds := credsByChannel[model.ChannelId]
		if len(creds) == 0 {
			continue
		}
		modelCombos := comboByModelCred[model.Id]
		views := make([]*CredentialHealthView, 0, len(creds))
		for _, cred := range creds {
			view := &CredentialHealthView{
				CredentialId: cred.Id,
				KeyPrefix:    cred.KeyPrefix,
				HealthScore:  100,
			}
			if modelCombos != nil {
				if combo, ok := modelCombos[cred.Id]; ok {
					view.HealthScore = combo.HealthScore
					view.CooldownUntil = combo.CooldownUntil
					view.LastError = sanitizeCredentialErrorMessage(combo.LastError)
				}
			}
			views = append(views, view)
		}
		result[model.Id] = views
	}
	return result, nil
}

// summarizeCredentialHealth 在排除「隔离/冷却中」组合后，返回可用 key 的最高分。
// 第二个返回值表示是否存在"可用组合"（即存在未处于冷却的有效 key）：
//   - 全部有效 key 都在冷却，或没有任何有效 key 时，返回 (0, false)；
//   - 否则返回 (最高可用分, true)。
//
// 该函数的判定与"当前时刻 now"相关，因此抽成纯函数便于单测，不触碰数据库。
func summarizeCredentialHealth(views []*CredentialHealthView, now *gtime.Time) (int, bool) {
	best := 0
	available := false
	for _, v := range views {
		// 隔离/冷却中的组合不计入"可用最高分"。
		if v.CooldownUntil != nil && v.CooldownUntil.TimestampMilli() > now.TimestampMilli() {
			continue
		}
		available = true
		if v.HealthScore > best {
			best = v.HealthScore
		}
	}
	return best, available
}

// sanitizeCredentialErrorMessage 对组合错误摘要脱敏，移除疑似密钥/令牌的片段。
// 错误文案由写入侧产生，本函数在只读展示路径上统一脱敏，不信任写入源，
// 避免上游把密钥回显到前端。仅做展示层防护，不改变写入侧字段。
func sanitizeCredentialErrorMessage(input string) string {
	if input == "" {
		return ""
	}
	// sk-/af_ 前缀密钥：保留前 6 位后打码。
	output := regexp.MustCompile(`(?i)\b(sk|af)[-_][A-Za-z0-9._\-]{6,}`).ReplaceAllStringFunc(input, func(m string) string {
		if len(m) <= 6 {
			return m
		}
		return m[:6] + "****"
	})
	// Bearer 令牌。
	output = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]+`).ReplaceAllString(output, "Bearer ****")
	// 长随机串（>=32 连续字母数字，疑似密文/base64/hex 令牌）。
	output = regexp.MustCompile(`[A-Za-z0-9+/]{32,}={0,2}`).ReplaceAllStringFunc(output, func(m string) string {
		if len(m) <= 8 {
			return m
		}
		return m[:8] + "****"
	})
	return output
}
