package channel

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

const credentialRemarkMaxLength = 500

// normalizeCredentialRemark 与 API 校验共用同一长度边界，避免内部调用绕过校验。
// 备注是普通展示文本，不解析 HTML，不接触密钥密文、状态或计费配置。
func normalizeCredentialRemark(value string) (string, error) {
	if utf8.RuneCountInString(value) > credentialRemarkMaxLength {
		return "", gerror.New("备注最多500个字符")
	}
	return strings.TrimSpace(value), nil
}

// SetCredentialRemark 修改指定渠道下的一把密钥，不能通过其它渠道ID修改它。
// 备注不参与路由选择，无需刷新路由或产生集群配置版本变更。
func (s *sChannel) SetCredentialRemark(ctx context.Context, channelID, credentialID uint64, input adminapi.ChannelCredentialRemarkInput) error {
	if err := s.ensureOwned(ctx, channelID); err != nil {
		return err
	}
	if input.Remark == nil {
		return gerror.New("请传入备注，清除备注请传空字符串")
	}
	remark, err := normalizeCredentialRemark(*input.Remark)
	if err != nil {
		return err
	}
	credential, err := s.credentialByID(ctx, channelID, credentialID)
	if err != nil {
		return err
	}
	if _, err = dao.ChannelCredentials.Ctx(ctx).
		Where(do.ChannelCredentials{Id: credential.Id, ChannelId: channelID}).
		Data(do.ChannelCredentials{Remark: remark}).Update(); err != nil {
		return gerror.Wrap(err, "update channel credential remark")
	}
	return nil
}
