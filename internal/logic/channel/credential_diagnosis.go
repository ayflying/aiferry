package channel

import (
	"context"
	"fmt"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/model/do"
)

// 选凭证阶段的诊断结论文案。这些字符串会进入用量记录的错误信息与 WARN 日志，
// 是排查「零上游尝试」时的第一手材料，措辞必须与真实过滤条件一致。
const (
	// credentialSkipReasonAvailable 表示渠道级检查认为密钥可用。它在 relay 侧单独出现时，
	// 说明密钥是被「本次请求」的排除集合挡下的，而不是渠道自身不可用。
	credentialSkipReasonAvailable = "有可用密钥"
	// credentialSkipReasonUnknown 是查库失败时的降级文案，不阻断主流程。
	credentialSkipReasonUnknown = "原因未知"
)

// CredentialUnavailableError 表示渠道层挑不出可用的上游密钥。Error() 文本保持历史口径
// （"channel has no available upstream credential"），Reason 给出可直接展示的具体原因。
//
// 之所以要把原因带出来、而不是让调用方按渠道重查一遍：组合冷却（「模型 × 密钥」维度）
// 只有 SelectCredential 内部才看得到，渠道级重查会得出「有可用密钥」，
// 于是日志写成「全部候选渠道凭证不可用：xxx: 有可用密钥」这种自相矛盾的说法，
// 把排查引向错误方向。
type CredentialUnavailableError struct {
	ChannelID uint64
	Reason    string
}

func (e *CredentialUnavailableError) Error() string {
	return "channel has no available upstream credential"
}

// CredentialSkipReason 返回渠道在选凭证阶段会被整体跳过的具体原因：
//   - "N 把密钥全部冷却中"：启用密钥存在但都在 Redis 冷却（连续失败触发）；
//   - "无启用密钥"：渠道没有 status=1 的密钥（被手动禁用或自动禁用）；
//   - "有可用密钥"：渠道级过滤解释不了——密钥是被本次请求的排除集合挡下的
//     （组合冷却由 SelectCredential 直接给出原因，不会落到这里）。
func (s *sChannel) CredentialSkipReason(ctx context.Context, channelID uint64) (string, error) {
	rows := make([]credentialRow, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID, Status: 1}).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&rows); err != nil {
		return "", gerror.Wrap(err, "list channel credentials for skip reason")
	}
	if len(rows) == 0 {
		return "无启用密钥", nil
	}
	cooling := 0
	for _, row := range rows {
		if value, err := s.app.Redis.TTL(ctx, system.CredentialCooldownKey(row.Id)).Result(); err == nil && value > 0 {
			cooling++
		}
	}
	if cooling == len(rows) {
		return fmt.Sprintf("%d 把密钥全部冷却中", cooling), nil
	}
	if cooling > 0 {
		return fmt.Sprintf("%d/%d 把密钥冷却中，其余不可选", cooling, len(rows)), nil
	}
	return credentialSkipReasonAvailable, nil
}

// credentialUnavailableError 在选密钥失败时构造带原因的错误。afterChannelFilters 是已通过
// 渠道级凭证冷却与排除集合过滤的密钥数：它大于 0 而最终无密钥可选，只可能是「模型 × 密钥」
// 组合冷却把全部密钥挡下了。excludedCount 是本次请求的排除集合大小，仅在渠道级结论为
// 「有可用密钥」时用于解释差异。
func (s *sChannel) credentialUnavailableError(ctx context.Context, channelID uint64, afterChannelFilters, excludedCount int) error {
	channelReason := ""
	if afterChannelFilters == 0 {
		// 渠道级过滤后已无密钥，才需要查库区分「无启用密钥」与「全部冷却」。
		var err error
		if channelReason, err = s.CredentialSkipReason(ctx, channelID); err != nil {
			channelReason = credentialSkipReasonUnknown
		}
	}
	return &CredentialUnavailableError{
		ChannelID: channelID,
		Reason:    credentialUnavailableReasonText(afterChannelFilters, excludedCount, channelReason),
	}
}

// credentialUnavailableReasonText 把过滤计数与渠道级原因合成最终展示文案。抽成纯函数便于单测。
func credentialUnavailableReasonText(afterChannelFilters, excludedCount int, channelReason string) string {
	switch {
	case afterChannelFilters > 0:
		return fmt.Sprintf("%d 把密钥在该模型下全部处于组合冷却", afterChannelFilters)
	case excludedCount > 0 && channelReason == credentialSkipReasonAvailable:
		// 渠道级看得到密钥，本次请求却一把都挑不出：排除集合是唯一解释。
		return "本次请求已排除该渠道全部可选密钥"
	default:
		return channelReason
	}
}
