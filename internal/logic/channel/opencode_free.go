package channel

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// OpenCode Zen 免费层指纹门禁的历史（实测来源：opencode2dsh 插件的在线
// 探测矩阵，https://github.com/FishBottle7/opencode2dsh）：
//   - 2026-09-16：会话必须满足 ses_ + 12hex + 14Base62 形状，纯 UA 前缀
//     不再足够，形状不符直接 403 FreeTierError；
//   - 2026-09-18：门禁补了请求体一半——必须流式且 tools 带 bash/read，
//     头与体两个半闸同时过闸才放行；
//   - 2026-10-13：hex 段不再是任意摘要——官方客户端编码的是按位取反的
//     毫秒时间戳（低 12 位为毫秒内计数器），上游反向解码后只接受计数器
//     [0,63]；摘要式随机值落窗概率约 1.5%，几乎必 403，免费车道全线
//     变红即此因。
//
// User-Agent 拼写与 Authorization 凭据（字面量 Bearer public 亦可）实测
// 不是门禁，但保持官方 CLI 同款形状，为后续可能新增的指纹检查留余量。
const openCodeClientVersion = "1.18.31"

// openCodeNodeVersion 是 CLI 同款 User-Agent 里的 Node 运行时版本，
// 仅用于凑齐官方客户端的 UA 形状，取值不参与上游判定。
const openCodeNodeVersion = "22.21.1"

// openCodeSessionPrefix 是免费层会话标识的固定前缀，后续必须跟
// 12 位 hex + 14 位 Base62（共 26 字符），且 hex 段反向解码出的计数器
// 必须落在 [0,63] 窗口，否则 403。
const openCodeSessionPrefix = "ses_"

// base62Alphabet 是会话标识后 14 位使用的字符集（0-9A-Za-z）。
const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// openCodeTimestampMask 是会话 hex 段承载的 48 位整数掩码：高 36 位
// 毫秒时钟 + 低 12 位毫秒内计数器，整体按位取反后编码为 12 位 hex。
const openCodeTimestampMask = 0xFFFFFFFFFFFF

// openCodeCounterMask 是低 12 位毫秒内计数器掩码。
const openCodeCounterMask = 0xFFF

// openCodeCounterMax 是上游接受的最大毫秒内计数器（实测窗口 [0,63]）。
const openCodeCounterMax = 63

// OpenCodeZenChannelType 是 OpenCode Zen 免费车道的渠道类型编码，由
// manifest/builtins.json 定义，与付费 Go 车道（opencode_go）相互独立：
// 两者的 baseUrl、套餐额度查询地址与客户端指纹策略都不同。
const OpenCodeZenChannelType = "opencode_zen"

// IsOpenCodeFreeLane 判定当前请求是否走 OpenCode Zen 免费车道指纹模拟。
// 只认渠道类型 opencode_zen：编辑器指纹（opencode/ UA、ses_ 会话、
// 工具桩）仅免费层需要；opencode_go 及其它类型即使地址指向 …/zen/v1 也
// 不模拟——付费 Go 车道维持 aiferry 会话与 UA 策略，由类型硬隔离。
func IsOpenCodeFreeLane(channelType, _ string) bool {
	return strings.TrimSpace(channelType) == OpenCodeZenChannelType
}

// applyOpenCodeFreeHeaders 为免费层请求补齐官方 CLI 同款请求头：
// opencode/ 开头的 User-Agent、会话标识三处同值（x-opencode-session、
// x-session-affinity、X-Session-Id，上游任一命中即可，与官方客户端保持
// 同形）、以及 x-opencode-client/request/project 相关标识。客户端自带
// 合规会话时透传，保住上游的会话亲和。
func applyOpenCodeFreeHeaders(target, incoming http.Header, identity UpstreamClientIdentity) {
	ua := ""
	if incoming != nil {
		ua = strings.TrimSpace(incoming.Get("User-Agent"))
	}
	if strings.HasPrefix(strings.ToLower(ua), "opencode/") {
		target.Set("User-Agent", ua)
	} else {
		target.Set("User-Agent", openCodeFreeUserAgent())
	}
	session := openCodeFreeSessionID(incoming, identity)
	target.Set(openCodeSessionHeader, session)
	target.Set("x-session-affinity", session)
	target.Set("X-Session-Id", session)
	target.Set("x-opencode-client", "cli")
	target.Set("x-opencode-request", openCodeRequestID())
	target.Set("x-opencode-project", openCodeProjectSegment())
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

// isOpenCodeFreeSessionID 校验会话是否满足官方客户端形状：
// ses_ + 12 位 hex + 14 位 Base62，且 hex 段按位取反解码后的低 12 位
// 计数器必须落在上游接受的 [0,63] 窗口（2026-10-13 新增校验：摘要式
// 随机值即使形状合规也几乎必 403）。hex 段放宽大小写以便透传。
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
	inverted, err := strconv.ParseUint(body[:12], 16, 64)
	if err != nil {
		return false
	}
	return ^inverted&openCodeCounterMask <= openCodeCounterMax
}

// openCodeFreeClockSegment 返回本进程共用的会话时钟段（12 位 hex）。
// 进程内首次使用时按当前毫秒时钟铸造、计数器固定为 1，此后所有派生
// 会话共享同一段：上游只校验低 12 位计数器，时钟位不参与判定；会话
// 之间靠 Base62 尾段区分，提示词缓存亲和不受影响。进程重启后重新铸造，
// 代价仅是上游缓存重新预热，与官方客户端重启的行为一致。
var (
	openCodeFreeClockOnce sync.Once
	openCodeFreeClockHex  string
)

func openCodeFreeClockSegment() string {
	openCodeFreeClockOnce.Do(func() {
		openCodeFreeClockHex = invertedTimestampHex(time.Now().UnixMilli(), 1)
	})
	return openCodeFreeClockHex
}

// invertedTimestampHex 把毫秒时钟编码成官方客户端的 12 位 hex 段：
// (毫秒<<12 | counter) 整体按位取反后转小写 hex，使新会话按字符串
// 倒序排列。上游会反向解码低 12 位校验计数器窗口，摘要式随机值不可用
// （opencode2dsh issue #62，2026-10-13 实测）。
func invertedTimestampHex(nowMilli int64, counter uint64) string {
	current := uint64(nowMilli)<<12 | counter&openCodeCounterMask
	inverted := ^current & openCodeTimestampMask
	return fmt.Sprintf("%012x", inverted)
}

// deriveOpenCodeFreeSessionID 派生官方客户端同款会话：
// ses_ + 12 位反码时间戳 hex + 14 位 Base62。时钟段进程内共享，
// Base62 尾段由渠道、密钥、模型（及转发用户）身份决定，保证同一身份
// 的会话跨请求稳定、不同身份互不冲突。
func deriveOpenCodeFreeSessionID(identity UpstreamClientIdentity) string {
	key := fmt.Sprintf("v1free|u:%d|c:%d|k:%d|m:%s", identity.UserID, identity.ChannelID, identity.CredentialID, identity.ModelName)
	digest := sha256.Sum256([]byte(key))
	base62Part := encodeBase62(digest[6:16], 14)
	return openCodeSessionPrefix + openCodeFreeClockSegment() + base62Part
}

// openCodeFreeUserAgent 返回官方 CLI 同款 User-Agent，形如
// opencode/1.18.31 (linux amd64; node22.21.1)。UA 拼写实测不是门禁，
// 但保持同款形状可避免未来按 UA 形状收紧时再被拦截。
func openCodeFreeUserAgent() string {
	return fmt.Sprintf("opencode/%s (%s %s; node%s)", openCodeClientVersion, goPlatformName(), goArchName(), openCodeNodeVersion)
}

// goPlatformName 把 Go 的 GOOS 映射成 Node 的 platform 拼写。
func goPlatformName() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	default:
		return runtime.GOOS
	}
}

// goArchName 把 Go 的 GOARCH 映射成 Node 的 arch 拼写。
func goArchName() string {
	if runtime.GOARCH == "amd64" {
		return "x64"
	}
	return runtime.GOARCH
}

// openCodeRequestID 生成单请求相关标识 req_ + 32 位 hex，与官方客户端
// 每请求随机的行为一致；上游不校验取值，仅保持链路形状同款。
func openCodeRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败时退化为纳秒时钟，保证标识仍然唯一。
		return fmt.Sprintf("req_%016x%016x", uint64(time.Now().UnixNano()), uint64(time.Now().UnixNano())>>1)
	}
	return "req_" + hex.EncodeToString(buf)
}

// openCodeProjectSegment 返回网关级稳定的项目标识 prj_ + 24 位 hex。
// 官方客户端按工作目录派生该值；网关没有工作目录概念，进程内固定一份。
var (
	openCodeProjectOnce         sync.Once
	openCodeProjectSegmentValue string
)

func openCodeProjectSegment() string {
	openCodeProjectOnce.Do(func() {
		digest := sha256.Sum256([]byte("aiferry:default-project"))
		openCodeProjectSegmentValue = "prj_" + hex.EncodeToString(digest[:12])
	})
	return openCodeProjectSegmentValue
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
// 复刻的最小工具桩，只用于通过免费层指纹校验；上游不检查其 schema
// 与描述，仅认函数名。
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
