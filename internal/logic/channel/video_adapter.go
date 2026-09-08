package channel

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// VideoAdapterFor 返回渠道类型声明的视频适配器（openai / minimax / volcengine_ark）。
// 类型不存在或配置缺失时回退为 openai（标准 /videos 端点）。
func (s *sChannel) VideoAdapterFor(ctx context.Context, channelTypeCode string) (string, error) {
	code := strings.TrimSpace(channelTypeCode)
	if code == "" {
		return channeltype.VideoAdapterOpenAI, nil
	}
	_, typeConfig, err := s.types.GetByCode(ctx, code)
	if err != nil {
		return "", gerror.Wrapf(err, "resolve video adapter for channel type %s", code)
	}
	if typeConfig.Video.Adapter == "" {
		return channeltype.VideoAdapterOpenAI, nil
	}
	return typeConfig.Video.Adapter, nil
}
