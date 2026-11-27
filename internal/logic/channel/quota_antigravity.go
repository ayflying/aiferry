package channel

import (
	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// resolveAntigravityQuotaURL 解析 Antigravity 桥接的额度端点：桥接的 /v1/quota
// 挂在渠道根地址（http://agy-bridge:8091/v1）之下，因此按「根地址 + 相对路径」
// 拼接，而不是智谱那类「host 根路径」规则——后者会把 /v1 丢掉拼出错地址。
// 配置了完整地址时直连，方便桥接跑在别的地址上。
func resolveAntigravityQuotaURL(baseURL, configured string) (string, error) {
	return resolveEndpointURL(baseURL, configured)
}

// parseAntigravityUsage 复用 OpenCode Go 用量解析器：桥接把官方 CLI 的 /usage
// 翻译成同一形状（usage.rolling = 5 小时窗口，usage.weekly = 每周窗口），
// 两边字段一一对应，不再复制一份解析实现。模式标记仍记为本适配器。
func parseAntigravityUsage(body []byte) (QuotaView, error) {
	view, err := parseOpenCodeGoUsage(body)
	if err != nil {
		return QuotaView{}, err
	}
	view.Mode = channeltype.AdapterAntigravityUsage
	return view, nil
}

// antigravityChannelType 是「Google Gemini（Antigravity 订阅）」渠道类型编码，
// 由 manifest/builtins.json 定义；转发链路据此决定是否把代理地址带给桥接。
const antigravityChannelType = "gemini_antigravity"

// AntigravityProxyHeader 是转发链路把「该密钥当前选中的代理地址」交给桥接的内部
// 请求头。桥接只在令牌认证通过后读取它，并据此给容器内的官方 agy 进程设置代理
// 环境；未配置代理的渠道不带此头，agy 直连。
const AntigravityProxyHeader = "X-AiFerry-Proxy"

// IsAntigravityChannel 判断渠道是否为经 Antigravity 桥接的 Gemini 订阅渠道。
// 转发链路据此决定是否把当前密钥选中的代理地址带给桥接。
func IsAntigravityChannel(channelType string) bool {
	return channelType == antigravityChannelType
}
