package channel

import (
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	adminapi "github.com/yunloli/aiferry/api/admin"
)

func validateGeminiSubscriptionInput(baseURL string, input adminapi.ChannelInput, hasProxy bool) error {
	if baseURL != GeminiSubscriptionBaseURL {
		return gerror.New("Gemini 订阅渠道只允许使用预设上游地址")
	}
	if input.ProxyURL != nil {
		hasProxy = strings.TrimSpace(*input.ProxyURL) != ""
	}
	if !hasProxy {
		return gerror.New("Gemini 订阅渠道必须配置代理；已阻止直连")
	}
	if strings.TrimSpace(input.ProjectID) == "" {
		return gerror.New("Gemini 订阅渠道必须填写经账号授权的 Google Cloud 项目 ID")
	}
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		return gerror.New("Gemini 订阅渠道请使用 Google 登录，不支持手工填写密钥")
	}
	return nil
}
