package relay

import (
	"context"
	"net/http"

	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

// applyUpstreamAuthHeaders 用渠道类型声明的鉴权规则写入上游鉴权头与基础头。
// 与模型测试、模型发现/同步链路共用渠道层实现：渠道类型的
// authType / headerName / headerPrefix 只解释一次，不会再出现
// "管理端测试通过、正式转发 401" 的两套行为。
// 渠道类型解析失败时回退为标准 Bearer 规则，保证转发不因此中断。
func (s *sRelay) applyUpstreamAuthHeaders(ctx context.Context, req *http.Request, candidate Candidate) error {
	spec, err := s.channels.UpstreamAuthSpecForChannelType(ctx, candidate.ChannelType)
	if err != nil {
		g.Log().Warningf(ctx, "resolve upstream auth for channel type %s: %v", candidate.ChannelType, err)
		spec = channel.DefaultUpstreamAuthSpec()
	}
	return s.channels.ApplyUpstreamAuthHeaders(req, channel.UpstreamAuthInput{
		Spec:                spec,
		CredentialCipher:    candidate.APIKeyCipher,
		ManagementKeyCipher: candidate.ManagementKeyCipher,
		OrganizationID:      candidate.OrganizationID,
		ProjectID:           candidate.ProjectID,
		// 转发链路允许无鉴权请求（如已签名的媒体下载地址），不因缺少密钥中断。
		TolerateMissingKey: true,
	})
}
