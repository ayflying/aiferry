package channel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// OpenCode Zen 免费层指纹门禁的历史（实测来源：opencode2dsh 插件与
// opencode2api 项目的在线探测，https://github.com/FishBottle7/opencode2dsh、
// https://github.com/jasonxu114514/opencode2api）：
//   - 2026-09-16：会话必须满足 ses_ + 12hex + 14Base62 形状，纯 UA 前缀
//     不再足够，形状不符直接 403 FreeTierError；
//   - 2026-09-18：门禁补了请求体一半——必须流式且 tools 带 shell/read，
//     头与体两个半闸同时过闸才放行；
//   - 2026-10-13：hex 段不再是任意摘要——官方客户端编码的是按位取反的
//     毫秒时间戳（低 12 位为毫秒内计数器），上游反向解码后只接受计数器
//     [0,63]；摘要式随机值落窗概率约 1.5%，几乎必 403，免费车道全线
//     变红即此因；
//   - 2026-10-14：上游把匿名推理从 https://opencode.ai/zen 迁到
//     https://opencode.ai/inference，门禁同时改判四处指纹：UA 只认
//     opencode/latest/<版本>/cli（旧的 "opencode/<版本> (<os> <arch>;
//     <runtime>)" 拼写即使其余指纹全对也 403）、新增 x-opencode-session-id
//     头、x-opencode-project 改为裸 40 位 hex、拒绝网关自造的
//     x-opencode-request；请求体侧工具桩必须携带官方真实定义（2.x 的
//     shell 拼写，详见 opencode_free_body.go），tool_choice:none 也会 403。

// openCodeClientVersion 是免费层 User-Agent 声称的官方客户端版本。
// 上游接受一个版本区间，只在开始拒绝旧版本时才需要跟进升位。
const openCodeClientVersion = "2.0.29"

// openCodeFreeUpstreamBase 是 2026-10-14 起匿名免费层的推理基址。旧的
// /zen 基址仅剩模型目录与用量查询仍可用（实测 /zen/v1/models 与
// /inference/v1/models 均返回 200），chat completions 必须走新基址。
const openCodeFreeUpstreamBase = "https://opencode.ai/inference"

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
// 官方工具定义）仅免费层需要；opencode_go 及其它类型即使地址指向
// …/zen/v1 也不模拟——付费 Go 车道维持 aiferry 会话与 UA 策略，由类型
// 硬隔离。
func IsOpenCodeFreeLane(channelType, _ string) bool {
	return strings.TrimSpace(channelType) == OpenCodeZenChannelType
}

// OpenCodeFreeUpstreamURL 把免费车道请求的上游地址改写到 2026-10-14 起
// 的匿名推理基址：基址指向 opencode.ai 时一律换用 openCodeFreeUpstreamBase，
// 端点不带 /v1/ 前缀时补上（兼容 …/zen/v1 与 …/zen 两种存量基址写法）。
// 端点是绝对 URL（渠道类型声明的完整地址）时原样返回；基址不是
// opencode.ai（本地 mock 联调）时原样拼接，保持离线测试能力。
func OpenCodeFreeUpstreamURL(baseURL, endpoint string) string {
	if channeltype.IsAbsoluteHTTPURL(endpoint) {
		return endpoint
	}
	if !strings.Contains(strings.ToLower(strings.TrimSpace(baseURL)), "opencode.ai") {
		return baseURL + endpoint
	}
	path := endpoint
	if !strings.HasPrefix(path, "/v1/") {
		path = "/v1" + path
	}
	return openCodeFreeUpstreamBase + path
}

// applyOpenCodeFreeHeaders 为免费层请求补齐官方 2.x 客户端同款请求头：
// opencode/latest/<版本>/cli 形状的 User-Agent、会话标识四处同值
// （x-opencode-session、x-opencode-session-id、x-session-affinity、
// X-Session-Id）、x-opencode-client 与 x-opencode-project 标识。2026-10-14
// 起上游按 UA 形状拒绝请求，旧的 "opencode/<版本> (<os> <arch>; <runtime>)"
// 拼写即使其余指纹全对也 403，因此不再透传客户端 UA。
func applyOpenCodeFreeHeaders(target, incoming http.Header, identity UpstreamClientIdentity) {
	target.Set("User-Agent", openCodeFreeUserAgent())
	session := openCodeFreeSessionID(incoming, identity)
	target.Set(openCodeSessionHeader, session)
	target.Set("x-opencode-session-id", session)
	target.Set("x-session-affinity", session)
	target.Set("X-Session-Id", session)
	target.Set("x-opencode-client", "cli")
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

// openCodeFreeUserAgent 返回官方 2.x 客户端同款 User-Agent：
// opencode/latest/<版本>/cli。2026-10-14 起上游按 UA 形状拒绝请求，
// 旧的 "opencode/<版本> (<os> <arch>; <runtime>)" 拼写直接 403。
func openCodeFreeUserAgent() string {
	return "opencode/latest/" + openCodeClientVersion + "/cli"
}

// openCodeProjectSegment 返回网关级稳定的项目标识：裸 40 位小写 hex。
// 2026-10-14 起上游按形状校验该头，prj_ 前缀形状会被拒绝（实测仅认
// 形状、不校验取值）；官方客户端按工作目录派生该值，网关没有工作目录
// 概念，进程内固定一份，哈希保证同一网关钉住同一个项目。
var (
	openCodeProjectOnce         sync.Once
	openCodeProjectSegmentValue string
)

func openCodeProjectSegment() string {
	openCodeProjectOnce.Do(func() {
		digest := sha256.Sum256([]byte("opencode-project\x00" + "aiferry:default-project"))
		openCodeProjectSegmentValue = hex.EncodeToString(digest[:20])
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
