package channel

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// WorkBuddy 官方客户端身份头（伪装层）。
//
// WHY。2026-10-27 生产实证（afreq_965cb90f，用户真实请求 3 次全 11128
// "request illegal"）：上游失败体的 displayTips 明确写着「这次调用没通过渠道校验，
// 可能不是官方客户端发出的」。同一份字节由本网关低频重放却 200 —— 报文内容
// 已被扰动层放行，被拒的是「请求看起来不像官方客户端」这一层。参考实现
// dsh-connect-workbuddy（5.11.0，issue #66 / #132）对此的做法是补齐官方客户端的
// 身份头：官方 UA、Origin/Referer、X-Product 与 conversation 身份三连。
//
// 三连的来历：上游 chat 端点在 system 开头规则之外还要求官方桌面客户端的会话
// 身份（X-Conversation-ID / -Request-ID / -Message-ID），缺任一即 11128。参考
// 实现逐个二分过官方的 applyGatewayHeaders，实测只有这三件必需——acp-connection-id、
// X-Agent-Intent、X-IDE-*、X-Private-Data、trace 头都不是门槛，三连到位即满足。
// 值本身不被校验来源，只要形状合法；仍按官方客户端的行为每次请求重新生成。
const (
	// workBuddyChatClientUA 是官方 CLI 渠道在 chat 契约下的 User-Agent，
	// 与参考实现 CLIENT_UA 同值（UA 里的版本是官方 CLI 版本，不是本网关版本）。
	workBuddyChatClientUA = "CLI/2.63.2 CodeBuddy/2.63.2"
	// workBuddyBillingOrigin 是官方 CN 控制面域名，官方客户端用它填 Origin/Referer。
	workBuddyBillingOrigin = "https://www.codebuddy.cn"
)

// workBuddyClientOnlyHeaders 是客户端（本场景下是 DSH）自报、但官方客户端不会
// 发送的标识头。补齐官方身份前必须清掉，否则上游会同时看到官方身份与第三方
// 标识，混合身份比缺失更容易被判定为非官方客户端。
//
// 注意顺序：这些头必须在写入官方身份之前删除。Go 的 http.Header 会规范化头名，
// 若先写 X-Conversation-ID 再删除同名的客户端头，会把刚写入的官方值一起删掉。
var workBuddyClientOnlyHeaders = []string{
	"X-Session-Id", "Session-Id", "Session_id",
	"X-Acp-Connection-Id", "Acp-Connection-Id",
	"X-Agent-Intent", "X-Agent-Purpose",
	"X-Ide-Type", "X-Ide-Name", "X-Ide-Version",
	"X-Private-Data",
}

// ApplyWorkBuddyOfficialHeaders 把上游请求伪装成官方 WorkBuddy 客户端发出的 chat
// 请求：官方 UA、Origin/Referer、X-Requested-With、X-Product 与 conversation 身份
// 三连，并清掉客户端自报的第三方会话标识。每次调用生成新的三连（官方客户端同样
// 每请求新生成，复用同一组 id 反而会伪造出并不存在的会话）。
//
// 上游只看形状、不追查 id 来源，因此这里不带任何密钥信息；X-User-Id 一类用户
// 身份在网关侧未知时，按参考实现发 X-No-* 约定头（官方 CLI 的同类写法）。
func ApplyWorkBuddyOfficialHeaders(target http.Header) {
	if target == nil {
		return
	}
	// 先清客户端自报的第三方标识，再写官方身份（头名规范化见上方注释）。
	for _, name := range workBuddyClientOnlyHeaders {
		target.Del(name)
	}
	target.Set("User-Agent", workBuddyChatClientUA)
	target.Set("Accept", "application/json, text/plain, */*")
	target.Set("X-Requested-With", "XMLHttpRequest")
	target.Set("Origin", workBuddyBillingOrigin)
	target.Set("Referer", workBuddyBillingOrigin+"/")
	target.Set("X-Product", "SaaS")
	if strings.TrimSpace(target.Get("X-User-Id")) == "" {
		target.Set("X-No-User-Id", "1")
	}
	if strings.TrimSpace(target.Get("X-Enterprise-Id")) == "" {
		target.Set("X-No-Enterprise-Id", "1")
	}
	if strings.TrimSpace(target.Get("X-Domain")) == "" {
		target.Set("X-No-Department-Info", "1")
	}
	messageID := workBuddyRequestID()
	target.Set("X-Conversation-ID", workBuddyRequestID())
	target.Set("X-Conversation-Request-ID", workBuddyRequestID())
	target.Set("X-Conversation-Message-ID", messageID)
	// 上游把 X-Request-ID 回显为 SSE id 并写进使用页，官方客户端让它等于 message id，
	// 保持同值可避免同一账号在使用页上显示成两个客户端。
	target.Set("X-Request-ID", messageID)
}

// workBuddyRequestID 生成官方客户端形态的请求标识：32 位小写十六进制的
// UUIDv7（前 48 位是毫秒时间戳，版本位 7，RFC 4122 变体位）。官方客户端的
// 请求 id 就是这个形状；无需保证全局唯一性，只要求每次请求都不同。
func workBuddyRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// 随机源不可用时退回时间戳填充，仍满足「每次不同」的要求，
		// 且绝不因为身份头生成失败而阻断转发。
		raw = [16]byte{}
	}
	millis := uint64(time.Now().UnixMilli())
	for i := 0; i < 6; i++ {
		raw[i] = byte(millis >> (8 * (5 - i)))
	}
	raw[6] = (raw[6] & 0x0f) | 0x70
	raw[8] = (raw[8] & 0x3f) | 0x80
	return hex.EncodeToString(raw[:])
}