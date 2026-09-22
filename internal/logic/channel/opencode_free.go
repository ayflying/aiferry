package channel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// openCodeFreeUserAgent 是 OpenCode Zen 免费层要求的客户端标识。
// 上游按 UA 前缀判定是否来自官方客户端，通用库名或 aiferry/* 都会
// 被判为 FreeTierError（HTTP 403）。
const openCodeFreeUserAgent = "opencode/1.18.31"

// openCodeSessionPrefix 是免费层会话标识的固定前缀，后续必须跟
// 12 位小写 hex + 14 位 Base62（共 26 字符），否则 403。
const openCodeSessionPrefix = "ses_"

// openCodeFreeLanePath 与 openCodeGoLanePath 用于按路径区分
// 免费车道（…/zen/v1）与付费 Go 车道（…/zen/go/v1）。
const (
	openCodeFreeLanePath = "/zen/v1"
	openCodeGoLanePath   = "/zen/go/"
)

// base62Alphabet 是会话标识后 14 位使用的字符集（0-9A-Za-z）。
const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// IsOpenCodeFreeLane 判定渠道地址是否指向 OpenCode Zen 免费车道。
// 免费层形如 https://opencode.ai/zen/v1，付费 Go 车道形如
// https://opencode.ai/zen/go/v1；先排除 /zen/go/ 再匹配 /zen/v1，
// 避免两类地址混淆。判定只看 baseUrl，与渠道类型编码解耦，
// 便于既有 opencode_go 渠道直接改地址切到免费层。
func IsOpenCodeFreeLane(baseURL string) bool {
	normalized := strings.ToLower(strings.TrimSpace(baseURL))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, openCodeGoLanePath) {
		return false
	}
	return strings.Contains(normalized, openCodeFreeLanePath)
}

// applyOpenCodeFreeHeaders 为免费层请求补齐客户端指纹三件套中的
// 请求头部分：强制 opencode/ 开头的 User-Agent，以及格式合法的
// ses_ 会话标识。客户端自带合规会话时透传，保住上游的会话亲和。
func applyOpenCodeFreeHeaders(target, incoming http.Header, identity UpstreamClientIdentity) {
	ua := ""
	if incoming != nil {
		ua = strings.TrimSpace(incoming.Get("User-Agent"))
	}
	if strings.HasPrefix(strings.ToLower(ua), "opencode/") {
		target.Set("User-Agent", ua)
	} else {
		target.Set("User-Agent", openCodeFreeUserAgent)
	}
	target.Set(openCodeSessionHeader, openCodeFreeSessionID(incoming, identity))
}

// openCodeFreeSessionID 优先透传客户端自带的合规 ses_ 会话；否则按
// 渠道、密钥、模型（以及转发时的用户）派生稳定标识。不能用随机值：
// 随机会话会让上游每次请求重新选路、丢掉会话亲和。
func openCodeFreeSessionID(incoming http.Header, identity UpstreamClientIdentity) string {
	if incoming != nil {
		for _, name := range clientSessionHeaders {
			if value := strings.TrimSpace(incoming.Get(name)); isOpenCodeFreeSessionID(value) {
				return value
			}
		}
	}
	return deriveOpenCodeFreeSessionID(identity)
}

// isOpenCodeFreeSessionID 校验会话是否满足免费层格式：
// ses_ + 12 位 hex + 14 位 Base62。hex 段放宽大小写以便透传。
func isOpenCodeFreeSessionID(value string) bool {
	if !strings.HasPrefix(value, openCodeSessionPrefix) {
		return false
	}
	body := value[len(openCodeSessionPrefix):]
	if len(body) != 26 {
		return false
	}
	for i := 0; i < 12; i++ {
		c := body[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	for i := 12; i < 26; i++ {
		c := body[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return false
		}
	}
	return true
}

// deriveOpenCodeFreeSessionID 从身份哈希派生 ses_ + 12hex + 14Base62。
func deriveOpenCodeFreeSessionID(identity UpstreamClientIdentity) string {
	key := fmt.Sprintf("v1free|u:%d|c:%d|k:%d|m:%s", identity.UserID, identity.ChannelID, identity.CredentialID, identity.ModelName)
	digest := sha256.Sum256([]byte(key))
	hexPart := hex.EncodeToString(digest[:6])
	base62Part := encodeBase62(digest[6:16], 14)
	return openCodeSessionPrefix + hexPart + base62Part
}

// encodeBase62 把字节序列转成定长 Base62 串；不足位用 '0' 左补。
func encodeBase62(data []byte, length int) string {
	value := new(big.Int).SetBytes(data)
	base := big.NewInt(62)
	mod := new(big.Int)
	out := make([]byte, length)
	for i := length - 1; i >= 0; i-- {
		value.DivMod(value, base, mod)
		out[i] = base62Alphabet[mod.Int64()]
		if value.Sign() == 0 {
			for j := i - 1; j >= 0; j-- {
				out[j] = '0'
			}
			break
		}
	}
	return string(out)
}

// ApplyOpenCodeFreeBody 为免费层 chat completions 请求注入请求体三件套：
// 强制 stream=true、补齐 bash/read 工具桩、原请求无工具时置
// tool_choice=none（防止模型调用桩工具）；同时剥离缓存控制字段，
// 避免严格校验的上游报 UNKNOWN_FIELD。返回值 forcedStream 表示
// 是否把客户端的非流式请求改成了流式，调用方据此对回包做 SSE 聚合。
func ApplyOpenCodeFreeBody(body []byte) (result []byte, forcedStream bool, err error) {
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, false, gerror.Wrap(err, "decode free lane body")
	}
	originalStream, _ := payload["stream"].(bool)
	removePromptCacheControls(payload)
	payload["stream"] = true
	forcedStream = !originalStream

	tools, _ := payload["tools"].([]any)
	originalHadTools := len(tools) > 0
	hasBash, hasRead := false, false
	for _, item := range tools {
		switch openCodeToolName(item) {
		case "bash":
			hasBash = true
		case "read":
			hasRead = true
		}
	}
	if !hasBash {
		tools = append(tools, openCodeFreeBashTool())
	}
	if !hasRead {
		tools = append(tools, openCodeFreeReadTool())
	}
	payload["tools"] = tools
	if !originalHadTools {
		payload["tool_choice"] = "none"
	}

	options, _ := payload["stream_options"].(map[string]any)
	if options == nil {
		options = make(map[string]any)
		payload["stream_options"] = options
	}
	options["include_usage"] = true

	result, err = json.Marshal(payload)
	return result, forcedStream, gerror.Wrap(err, "encode free lane body")
}

// openCodeToolName 从 OpenAI 工具对象里取出函数名。
func openCodeToolName(item any) string {
	object, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	if function, ok := object["function"].(map[string]any); ok {
		name, _ := function["name"].(string)
		return name
	}
	name, _ := object["name"].(string)
	return name
}

// openCodeFreeBashTool / openCodeFreeReadTool 是按官方客户端抓包
// 复刻的最小工具桩，只用于通过免费层指纹校验。
func openCodeFreeBashTool() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "bash",
			"description": "Run a shell command",
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
				"required":   []string{"command"},
			},
		},
	}
}

func openCodeFreeReadTool() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "read",
			"description": "Read a file",
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			},
		},
	}
}
