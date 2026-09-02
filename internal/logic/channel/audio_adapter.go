package channel

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// AudioAdapterFor 返回渠道类型声明的音频适配器（openai / chat）。
// 类型不存在或配置缺失时回退为 openai（标准 /audio/* 端点）。
func (s *sChannel) AudioAdapterFor(ctx context.Context, channelTypeCode string) (string, error) {
	code := strings.TrimSpace(channelTypeCode)
	if code == "" {
		return channeltype.AudioAdapterOpenAI, nil
	}
	_, typeConfig, err := s.types.GetByCode(ctx, code)
	if err != nil {
		return "", gerror.Wrapf(err, "resolve audio adapter for channel type %s", code)
	}
	if typeConfig.Audio.Adapter == "" {
		return channeltype.AudioAdapterOpenAI, nil
	}
	return typeConfig.Audio.Adapter, nil
}
