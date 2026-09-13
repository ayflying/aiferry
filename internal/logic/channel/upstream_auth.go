package channel

import (
	"context"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// UpstreamAuthSpec 描述渠道类型声明的上游鉴权规则，来源于
// channeltype.Config.Models（正式转发、模型测试、模型发现共用同一组字段）。
type UpstreamAuthSpec struct {
	AuthType     string
	HeaderName   string
	HeaderPrefix string
}

// UpstreamAuthInput 是一次上游请求写入鉴权头所需的全部输入。
type UpstreamAuthInput struct {
	// Spec 是渠道类型声明的鉴权规则；留空时不做鉴权头处理。
	Spec UpstreamAuthSpec
	// CredentialCipher 是凭证级渠道密钥密文，AuthType=channel_key 时使用。
	CredentialCipher string
	// ManagementKeyCipher 是渠道管理密钥密文，AuthType=management_key 时使用；
	// 凭证自带管理密钥时，调用方应先用 withCredentialManagementKey 覆盖渠道级值。
	ManagementKeyCipher string
	// OrganizationID / ProjectID 是 OpenAI 兼容渠道的组织与项目标识。
	OrganizationID string
	ProjectID      string
	// TolerateMissingKey 为 true 时缺少密钥不报错。转发链路允许无鉴权请求
	// （例如已签名的媒体下载地址），管理端测试与同步保持 false，尽早暴露配置错误。
	TolerateMissingKey bool
}

// DefaultUpstreamAuthSpec 是所有内置渠道类型使用的标准鉴权规则
// （Authorization: Bearer <渠道密钥>）。渠道类型解析失败时作为兜底，
// 保证转发不因类型配置缺失而中断。
func DefaultUpstreamAuthSpec() UpstreamAuthSpec {
	return UpstreamAuthSpec{
		AuthType:     channeltype.AuthChannelKey,
		HeaderName:   "Authorization",
		HeaderPrefix: "Bearer ",
	}
}

// UpstreamAuthSpecForChannelType 解析渠道类型声明的上游鉴权规则。
// 内置类型走内存表，自定义类型走缓存，配置变更后转发与测试同时生效。
func (s *sChannel) UpstreamAuthSpecForChannelType(ctx context.Context, channelTypeCode string) (UpstreamAuthSpec, error) {
	code := strings.TrimSpace(channelTypeCode)
	if code == "" {
		return DefaultUpstreamAuthSpec(), nil
	}
	_, typeConfig, err := s.types.GetByCode(ctx, code)
	if err != nil {
		return UpstreamAuthSpec{}, gerror.Wrapf(err, "resolve upstream auth for channel type %s", code)
	}
	return UpstreamAuthSpec{
		AuthType:     typeConfig.Models.AuthType,
		HeaderName:   typeConfig.Models.HeaderName,
		HeaderPrefix: typeConfig.Models.HeaderPrefix,
	}, nil
}

// ApplyUpstreamAuthHeaders 按渠道类型声明的鉴权规则，把鉴权头与基础头写入上游
// 请求。正式转发（relay）与管理端模型测试、模型发现/同步共用这一实现：
// 渠道类型的 authType / headerName / headerPrefix 只在这里解释一次，
// 配置变更不会再出现"测试通过、正式转发 401"的两套行为。
func (s *sChannel) ApplyUpstreamAuthHeaders(req *http.Request, in UpstreamAuthInput) error {
	// 客户端已带 Accept 时保持原样（转发链路可能是 text/event-stream），
	// 否则补默认的 application/json。
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	switch in.Spec.AuthType {
	case channeltype.AuthNone:
	case channeltype.AuthChannelKey:
		key, err := s.upstreamAuthKey(in.CredentialCipher, in.TolerateMissingKey)
		if err != nil {
			return err
		}
		setUpstreamAuthHeader(req, in.Spec, key)
	case channeltype.AuthManagementKey:
		if in.ManagementKeyCipher == "" {
			if !in.TolerateMissingKey {
				return gerror.New("channel type requires a management key")
			}
		} else {
			key, err := s.upstreamAuthKey(in.ManagementKeyCipher, in.TolerateMissingKey)
			if err != nil {
				return err
			}
			setUpstreamAuthHeader(req, in.Spec, key)
		}
	default:
		if !in.TolerateMissingKey {
			return gerror.New("unsupported channel type auth")
		}
	}
	if in.OrganizationID != "" {
		req.Header.Set("OpenAI-Organization", in.OrganizationID)
	}
	if in.ProjectID != "" {
		req.Header.Set("OpenAI-Project", in.ProjectID)
	}
	return nil
}

// upstreamAuthKey 解密上游鉴权使用的密钥；cipher 为空时按 tolerate 决定是否报错。
func (s *sChannel) upstreamAuthKey(cipher string, tolerate bool) (string, error) {
	if cipher == "" {
		if tolerate {
			return "", nil
		}
		return "", gerror.New("channel credential is required")
	}
	return s.app.Secrets.Decrypt(cipher)
}

// setUpstreamAuthHeader 按渠道类型声明的头名与前缀写入密钥；密钥为空则跳过。
func setUpstreamAuthHeader(req *http.Request, spec UpstreamAuthSpec, key string) {
	if key == "" || strings.TrimSpace(spec.HeaderName) == "" {
		return
	}
	req.Header.Set(spec.HeaderName, spec.HeaderPrefix+key)
}
