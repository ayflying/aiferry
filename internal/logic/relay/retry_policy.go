package relay

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// sameCandidateRetryDelay 判断一次「挑选密钥失败」或「禁用类失败」是否应改判为同候选
// 退避重试，并给出本次时长：只有最后一个候选启用相应开关，且上一次真实尝试
// 命中限流、风控或暂时性服务错误时才允许重试；各类错误共享已消耗的退避计数，
// 且客户端仍在等待时才成立。429 限流维持单次退避（避免对上游限流放大压力）；风控拦截
// 按升级节奏最多 4 次——拦截按窗口成簇出现，同一窗口内重发仍会被拦，必须跨越窗口。
// 暂时性 500/502/503/504 最多退避 3 次；任何已提交输出或下游写失败都不能重放。
// 抽成纯函数便于单测覆盖边界。
func sameCandidateRetryDelay(options attemptOptions, retries int, last channelAttempt, ctxErr error) (time.Duration, bool) {
	if last.candidate.ChannelCredentialID <= 0 || ctxErr != nil || last.result.wroteBytes || last.result.writerFailed {
		return 0, false
	}
	if options.throttleRetry && last.result.status == http.StatusTooManyRequests && !isQuotaExhaustedResult(last.result) {
		if retries == 0 {
			return candidateThrottleRetryDelay, true
		}
		return 0, false
	}
	if options.riskControlRetry && isRiskControlBlockedResult(last.result) && retries < len(riskControlRetryDelays) {
		return riskControlRetryDelays[retries], true
	}
	// 显式禁用规则优先：不能借 5xx 兜底绕过管理员的渠道禁用判定。
	if options.transientRetry && !last.channelDead && !last.result.streamCompleted && isTransientUpstreamFailure(last.result) && retries < len(transientRetryDelays) {
		return transientRetryDelays[retries], true
	}
	return 0, false
}

// transientRetryDelays 限制最后一条路径的额外压力：最多重试 3 次，共退避 16.5 秒。
// 每次仍受上游超时与客户端上下文约束，不保证持续过载一定能恢复。
var transientRetryDelays = [...]time.Duration{1500 * time.Millisecond, 5 * time.Second, 10 * time.Second}

// isTransientUpstreamFailure 只认常见暂时性服务错误；501/505 等能力或协议错误不反复重放。
// 流式 error 帧在尚未提交输出时也会被规范化为这些状态码。
func isTransientUpstreamFailure(result attemptResult) bool {
	switch result.status {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// riskControlRetryDelays 是内容风控拦截的升级退避节奏。生产 8 天观测 11 个拦截窗口，
// 跨度 0~67s（中位 ~5s）：1.5s/5s/15s/30s 的累计 ~52s 覆盖其中 10 个窗口，仅放过
// 最长的一个 67s 窗口尾段。被拦请求本身响应极快（<1s），等待成本集中在退避本身。
var riskControlRetryDelays = [4]time.Duration{
	1500 * time.Millisecond,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
}

// riskControlBlockedMarkers 是上游内容风控「概率性拦截」的错误签名（如 workbuddy 渠道的
// code=11128「request illegal」）。拦截按窗口成簇：同一窗口内重发仍会被拦，因此同候选
// 按 riskControlRetryDelays 升级退避跨越窗口；上游 displayTips 亦建议「请重新发送」。
// 签名同时覆盖 upstreamerror.Message 的格式化文案与上游原始响应体两种形态。
var riskControlBlockedMarkers = []string{
	`code=11128`,      // 格式化文案里的风控码（code=11128）
	`request illegal`, // 风控 msg 原文（文案与原始响应体均含）
	`请求被拦截`,           // displayMsg 中文文案（部分上游只给文案）
}

// isRiskControlBlockedResult 判断一次失败是否为上游内容风控的概率性拦截：仅 400/422 且
// 错误文案（errorMessage，缺省回退原始响应体）命中风控签名。推理内容校验等确定性 400
// 不带这些签名，保持原有「换渠道/透传」语义，绝不会被误判为可重试。
func isRiskControlBlockedResult(result attemptResult) bool {
	if result.status != http.StatusBadRequest && result.status != http.StatusUnprocessableEntity {
		return false
	}
	message := result.errorMessage
	if strings.TrimSpace(message) == "" {
		message = string(result.body)
	}
	for _, marker := range riskControlBlockedMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// waitForRetry 等待一次同候选退避窗口（429 限流单次 1.5s；风控拦截按升级节奏逐次加长）。
// 客户端在退避期间断开时返回 false，调用方按原错误路径收尾，不再发起上游请求。
func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
