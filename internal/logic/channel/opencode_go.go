package channel

import "net/http"

// OpenCode Go（OpenCode Zen 订阅）渠道的上游客户端身份头。
//
// 该渠道要求稳定的会话标识头 x-opencode-session，缺失时上游直接返回
// HTTP 400 type="MissingSessionID"，请求不会进入模型推理。这里维持
// aiferry 会话与 UA 策略：即使地址指向 …/zen/v1，也不模拟免费层编辑器指纹
// （免费层指纹见 opencode_free.go，二者刻意不同——模拟免费层会触发付费层
// 的客户端治理判定）。
func applyOpenCodeGoHeaders(target, incoming http.Header, identity UpstreamClientIdentity) {
	target.Set(openCodeSessionHeader, upstreamSessionID(incoming, identity))
	applyUpstreamUserAgent(target, incoming)
}
