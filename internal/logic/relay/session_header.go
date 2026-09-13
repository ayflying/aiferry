package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// openCodeGoChannelType 是 OpenCode Go（OpenCode Zen 订阅）渠道类型编码，
// 由 manifest/builtins.json 定义。
const openCodeGoChannelType = "opencode_go"

// openCodeSessionHeader 是 OpenCode Go 强制要求的会话标识头。
// 上游用它做请求路由与提示词缓存优化，缺失时直接返回
// HTTP 400 type="MissingSessionID"，不会进入模型推理。
const openCodeSessionHeader = "x-opencode-session"

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

// applyUpstreamSessionHeader 为要求会话标识的上游渠道补齐会话头。
// 目前只有 OpenCode Go 有该要求，其余渠道不受影响。
func applyUpstreamSessionHeader(target, incoming http.Header, candidate Candidate, userID uint64) {
	if candidate.ChannelType != openCodeGoChannelType {
		return
	}
	target.Set(openCodeSessionHeader, upstreamSessionID(incoming, candidate, userID))
}

// upstreamSessionID 优先透传客户端原生会话标识；客户端未提供时退化为按
// 用户、渠道、密钥、模型维度派生的稳定标识。这里不能使用随机值：随机
// 会话会让上游每次请求都重新选择路由、丢掉提示词缓存命中。
func upstreamSessionID(incoming http.Header, candidate Candidate, userID uint64) string {
	if incoming != nil {
		for _, name := range clientSessionHeaders {
			if value := strings.TrimSpace(incoming.Get(name)); value != "" {
				return value
			}
		}
	}
	identity := fmt.Sprintf("v1|u:%d|c:%d|k:%d|m:%s", userID, candidate.ChannelID, candidate.ChannelCredentialID, candidate.PublicName)
	digest := sha256.Sum256([]byte(identity))
	return "aiferry-" + hex.EncodeToString(digest[:16])
}
