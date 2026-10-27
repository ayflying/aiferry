package channel

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/system"
)

// applyCredentialCostHealthCheck 让「查询余额/积分」的结果进入自动禁用链路。
//
// 0.6.36 新增。此前只有真实转发请求和模型测试会触发禁用判定，于是一个余额、
// 积分已经耗尽（上游固定返回 401/402 或 insufficient balance 文案）的账号，
// 只有等真实用户请求打到它、把失败算到用户头上之后才会被自动禁用。管理端点了
// 「查询余额」本身就拿到了上游的明确结论，正是最该在这里判定的时机。
//
// 复用已有的 DisableIfNeeded：状态码规则、连续失败阈值、密钥自动禁用与恢复、
// 通知、来源白名单全部沿用，不新增任何一套判定，避免两处规则漂移。
//
// 源码沿用 system.AutoDisableSourceRelayRequest（"relay_request"）：上游
// auto_disable_source 的取值白名单只认 relay_request 与 model_test，内部再细分
// 也会被归一成 unknown，反而丢失「这是真实上游结论」的语义。
//
// 失败不影响查询结果：余额查询是只读展示操作，禁用判定出错只在日志里留痕。
func (s *sChannel) applyCredentialCostHealthCheck(ctx context.Context, channelID, credentialID uint64, status int, message string) {
	if s.resilience == nil || credentialID == 0 {
		return
	}
	if _, err := s.resilience.DisableIfNeeded(ctx, system.AutoDisableInput{
		ChannelID:           channelID,
		ChannelCredentialID: credentialID,
		Source:              system.AutoDisableSourceRelayRequest,
		Status:              status,
		Message:             message,
	}); err != nil {
		g.Log().Warningf(ctx, "cost query auto-disable check for credential %d failed: %v", credentialID, err)
	}
}

// credentialCostFailureStatus 从查询错误里恢复出上游 HTTP 状态码，供自动禁用
// 判定使用；拿不到状态码（网络错误、解析失败、未配置管理密钥）时返回 0，
// 判定只看错误文案，不会被误判成鉴权失败。
func credentialCostFailureStatus(err error) int {
	if err == nil {
		return 0
	}
	return upstreamHTTPStatus(err.Error())
}

// upstreamHTTPStatus 从 "… HTTP 401 …" 这样的错误文案里取出状态码；这是
// fetchUpstreamJSON 的统一错误格式，取不到返回 0。
func upstreamHTTPStatus(message string) int {
	const marker = "HTTP "
	for index := 0; index+len(marker) <= len(message); index++ {
		if message[index:index+len(marker)] != marker {
			continue
		}
		status := 0
		digits := 0
		for _, char := range message[index+len(marker):] {
			if char < '0' || char > '9' {
				break
			}
			status = status*10 + int(char-'0')
			digits++
		}
		if digits == 3 {
			return status
		}
	}
	return 0
}

// costQuerySkipAutoDisable 判断该适配器是否跳过自动禁用判定。
//
// WorkBuddy 渠道的推理密钥是登录换来的 accessToken，过期后积分接口同样返回
// 401；但这个 token 会由平台登录链路自动续期，把账号密钥（以及背后的整条渠道）
// 禁用掉反而会让用户「明明登录着却用不了」，收益为负。它仍然照常记录错误与
// 取消余额快照，只是不参与禁用判定。
func costQuerySkipAutoDisable(adapter string) bool {
	return adapter == channeltype.AdapterWorkBuddy
}
