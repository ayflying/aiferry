package channel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/yunloli/aiferry/internal/buildinfo"
)

// OpenCodeGoChannelType 是 OpenCode Go（OpenCode Zen 订阅）渠道类型编码，
// 由 manifest/builtins.json 定义。
const OpenCodeGoChannelType = "opencode_go"

// openCodeSessionHeader 是 OpenCode Go 强制要求的会话标识头。
// 上游用它做请求路由与提示词缓存优化，缺失时直接返回
// HTTP 400 type="MissingSessionID"，请求不会进入模型推理。
const openCodeSessionHeader = "x-opencode-session"

// gatewayUserAgentPrefix 是 AiFerry 覆盖通用 SDK / HTTP 库 User-Agent 时
// 使用的前缀，实际取值形如 aiferry/0.5.93。
const gatewayUserAgentPrefix = "aiferry/"

// clientSessionHeaders 是各编码代理使用的原生会话头，按优先级依次探测。
// OpenCode Go 文档明确要求代理转发时保留客户端会话标识，
// 因此下游带了任一名称的会话头时优先原样透传。
var clientSessionHeaders = []string{
	"x-opencode-session",
	"session_id",
	"session-id",
	"x-session-id",
	"conversation_id",
	"conversation-id",
	"x-conversation-id",
}

// genericUserAgentMarkers 命中即视为"通用 SDK / HTTP 库"的 User-Agent。
// OpenCode Go 要求客户端用自己的客户端名标识自己，通用库名会被判定为
// 非编码代理流量；比较前统一转为小写，避免大小写差异漏判。
var genericUserAgentMarkers = []string{
	"openai/", "openai-python", "openai-node", "openai-dotnet", "openai-go",
	"anthropic/", "anthropic-sdk", "google-generativeai", "generative-language-client",
	"python-requests", "python-urllib", "urllib", "httpx", "aiohttp", "requests/",
	"axios", "node-fetch", "nodejs", "node.js", "undici", "dio/", "dart",
	"go-http-client", "okhttp", "java/", "httpclient", "apache-httpclient", "reactor-netty",
	"guzzle", "rest-client", "ruby", "libwww-perl", "httpie",
	"curl/", "wget/", "postmanruntime", "insomnia",
	"mozilla/", "chrome/", "safari/", "edg/", "firefox/",
}

// UpstreamClientIdentity 描述一次上游请求的客户端身份，用于派生稳定会话标识。
// UserID 为 0 时（如管理端测试与巡检）只按渠道、密钥、模型派生。
type UpstreamClientIdentity struct {
	ChannelType  string
	ChannelID    uint64
	CredentialID uint64
	ModelName    string
	UserID       uint64
}

// ApplyUpstreamClientHeaders 按渠道类型补齐上游要求的客户端标识头。
// 目前只有 OpenCode Go 有要求（稳定会话标识 + 拒绝通用库 UA），
// 其余渠道不受影响，转发与测试链路都必须调用，否则该渠道
// 会因缺少会话标识被上游直接拒绝。
func ApplyUpstreamClientHeaders(target, incoming http.Header, identity UpstreamClientIdentity) {
	if identity.ChannelType != OpenCodeGoChannelType {
		return
	}
	target.Set(openCodeSessionHeader, upstreamSessionID(incoming, identity))
	applyUpstreamUserAgent(target, incoming)
}

// upstreamSessionID 优先透传客户端原生会话标识；客户端未提供时退化为按
// 渠道、密钥、模型（以及转发时的用户）维度派生的稳定标识。这里不能使用
// 随机值：随机会话会让上游每次请求都重新选择路由、丢掉提示词缓存命中。
func upstreamSessionID(incoming http.Header, identity UpstreamClientIdentity) string {
	if incoming != nil {
		for _, name := range clientSessionHeaders {
			if value := strings.TrimSpace(incoming.Get(name)); value != "" {
				return value
			}
		}
	}
	identityKey := fmt.Sprintf("v1|u:%d|c:%d|k:%d|m:%s", identity.UserID, identity.ChannelID, identity.CredentialID, identity.ModelName)
	digest := sha256.Sum256([]byte(identityKey))
	return "aiferry-" + hex.EncodeToString(digest[:16])
}

// applyUpstreamUserAgent 只在客户端未标识自己（空 UA）或使用通用 SDK / HTTP
// 库名时，把 User-Agent 换成 AiFerry 自己的标识。真实编码代理（codex、
// claude-cli、opencode 等）的 UA 原样保留，便于上游做客户端维度治理。
func applyUpstreamUserAgent(target, incoming http.Header) {
	userAgent := ""
	if incoming != nil {
		userAgent = strings.TrimSpace(incoming.Get("User-Agent"))
	}
	if userAgent != "" && !isGenericUserAgent(userAgent) {
		target.Set("User-Agent", userAgent)
		return
	}
	target.Set("User-Agent", gatewayUserAgentPrefix+buildinfo.RuntimeVersion())
}

func isGenericUserAgent(userAgent string) bool {
	lowered := strings.ToLower(strings.TrimSpace(userAgent))
	if lowered == "" {
		return true
	}
	for _, marker := range genericUserAgentMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}
