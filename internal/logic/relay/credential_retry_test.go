package relay

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

// 选凭证失败的原因必须从渠道层带出来（渠道层才看得到「模型 × 密钥」组合冷却），
// 而不是事后按渠道重查；不认识的错误不得把内部错误原文写进排障文案。
func TestCredentialSkipReasonFromError(t *testing.T) {
	direct := credentialSkipReasonFromError(&channel.CredentialUnavailableError{ChannelID: 31, Reason: "3 把密钥在该模型下全部处于组合冷却"})
	if direct != "3 把密钥在该模型下全部处于组合冷却" {
		t.Fatalf("direct reason = %q", direct)
	}
	wrapped := credentialSkipReasonFromError(gerror.Wrap(&channel.CredentialUnavailableError{ChannelID: 31, Reason: "无启用密钥"}, "select credential"))
	if wrapped != "无启用密钥" {
		t.Fatalf("wrapped reason = %q", wrapped)
	}
	if unknown := credentialSkipReasonFromError(errors.New("redis: connection refused")); unknown != "" {
		t.Fatalf("unknown error should yield empty reason, got %q", unknown)
	}
	if nilReason := credentialSkipReasonFromError(nil); nilReason != "" {
		t.Fatalf("nil error should yield empty reason, got %q", nilReason)
	}
}

// 零尝试 503 的文案按渠道汇总真实原因，同渠道只出现一次，缺原因时不写空串。
func TestSummarizeCandidateSkips(t *testing.T) {
	candidates := []Candidate{
		{ChannelID: 31, ChannelName: "opencodeGo"},
		{ChannelID: 25, ChannelName: "OpenRouter"},
		{ChannelID: 31, ChannelName: "opencodeGo"},
	}
	reasons := map[uint64]string{
		31: "3 把密钥在该模型下全部处于组合冷却",
		25: "无启用密钥",
	}
	want := "opencodeGo(#31): 3 把密钥在该模型下全部处于组合冷却；OpenRouter(#25): 无启用密钥"
	if got := summarizeCandidateSkips(candidates, reasons); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if got := summarizeCandidateSkips([]Candidate{{ChannelID: 9, ChannelName: "solo"}}, nil); got != "solo(#9): 原因未知" {
		t.Fatalf("missing reason fallback = %q", got)
	}
}

// 渠道失效判定只认两条规则：状态码命中自动禁用状态码（DisableStatusCodes），或错误
// 文案命中失败关键词（FailureKeywords）。其余失败（普通 400、403、404、402、5xx）一律
// 留在重试链里换候选自愈：上游会把渠道侧故障包装成普通 400（如聚合层的 "Upstream
// request failed"），4xx 不能再当成对客户端请求的最终判决。
func TestChannelDeadFailureClassification(t *testing.T) {
	settings := system.DefaultResilienceSettings()
	// DisableStatusCodes 默认 401,429：命中即整渠道跳过，不再消耗本渠道的备用地址与密钥。
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		if !channelDeadFailure(attemptResult{status: status}, settings) {
			t.Fatalf("status %d listed in DisableStatusCodes must mark the channel dead", status)
		}
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusPaymentRequired, http.StatusInternalServerError} {
		if channelDeadFailure(attemptResult{status: status}, settings) {
			t.Fatalf("status %d must stay retryable under default settings", status)
		}
	}
	// 上游文件服务 404 被包成 HTTP 400 的情形（Responses 文件下载）：同样留在重试链里。
	fileDownload404 := attemptResult{status: http.StatusBadRequest, body: []byte(`{"error":{"code":"invalid_value","message":"Error while downloading file. Upstream status code: 404.","param":"url"}}`)}
	if channelDeadFailure(fileDownload404, settings) {
		t.Fatal("a file-download 404 wrapped in HTTP 400 must stay retryable")
	}
	// 失败关键词命中：即使状态码不在禁用清单里也整渠道跳过（大小写不敏感）。
	if !channelDeadFailure(attemptResult{
		status:       http.StatusBadRequest,
		errorMessage: "ERROR: YOUR CREDIT BALANCE IS TOO LOW",
	}, settings) {
		t.Fatal("a FailureKeywords hit must mark the channel dead regardless of status")
	}
	// 管理端把 403 加进 DisableStatusCodes 后同样生效。
	custom := settings
	custom.DisableStatusCodes = "403"
	if !channelDeadFailure(attemptResult{status: http.StatusForbidden}, custom) {
		t.Fatal("403 must mark the channel dead when listed in DisableStatusCodes")
	}
	// 已写出内容的结果由 attemptCompleted 就地收尾，不参与分类；空错误文案不参与关键词匹配。
	if channelDeadFailure(attemptResult{status: http.StatusUnauthorized, wroteBytes: true}, settings) {
		t.Fatal("written results must not be classified")
	}
	if channelDeadFailure(attemptResult{status: http.StatusBadRequest}, settings) {
		t.Fatal("a 400 without keywords must stay retryable")
	}
}

// reasoning_content 会话校验拒绝不再有专门豁免：按「其余情况一律重试」并入普通重试流
// （备用地址→换密钥→换候选渠道）。管理员若希望这类失败直接跳渠道，可把字段名加入
// FailureKeywords——关键词通道对 errorMessage 生效。
func TestStatefulSessionRejectionStaysRetryable(t *testing.T) {
	settings := system.DefaultResilienceSettings()
	cases := map[string]string{
		"deepseek": "Error from provider (Console Go): Upstream request failed: [invalid_request_error] The reasoning_content in the thinking mode must be passed back to the API.",
		"kimi":     "thinking is enabled but reasoning_content is missing in assistant tool call message at index 2",
	}
	for name, message := range cases {
		if channelDeadFailure(attemptResult{status: http.StatusBadRequest, errorMessage: message}, settings) {
			t.Fatalf("%s: stateful session rejection must stay retryable by default", name)
		}
	}
	keywords := settings
	keywords.FailureKeywords = append(keywords.FailureKeywords, "reasoning_content")
	for name, message := range cases {
		if !channelDeadFailure(attemptResult{status: http.StatusBadRequest, errorMessage: message}, keywords) {
			t.Fatalf("%s: adding the marker to FailureKeywords must mark the channel dead", name)
		}
	}
}

// 402（余额不足）不在默认禁用规则里：按普通重试流换同渠道其余密钥、再换候选渠道自愈；
// 密钥级禁用仍由 maybeAutoDisable 的 definitiveCredentialFailure 负责。
func TestPaymentRequiredStaysRetryableByDefault(t *testing.T) {
	settings := system.DefaultResilienceSettings()
	result := attemptResult{
		status:       http.StatusPaymentRequired,
		body:         []byte(`{"error":{"code":"INSUFFICIENT_BALANCE","message":"余额不足"}}`),
		errorMessage: "error: code=\"INSUFFICIENT_BALANCE\" message=\"余额不足\"",
	}
	if channelDeadFailure(result, settings) {
		t.Fatal("402 must stay retryable under default settings")
	}
	// 已写出内容后收到的失败无法重放：attemptCompleted 先行短路，不进入换候选决策。
	written := attemptResult{status: http.StatusPaymentRequired, wroteBytes: true}
	if !attemptCompleted(written, nil, true) {
		t.Fatal("a 402 after bytes were written must end the attempt")
	}
}

// 协议回退会对同一候选发起两次真实上游调用（首跳失败 + 回退结果）：两次都必须进入
// 调用流程，「上游尝试次数」也要按两步计——否则用户看到「上游尝试 1 次」，不知道
// 网关换过端点重试并自愈。
// 429 限流退避重试只在「最后一个候选、且本候选已无别的密钥可换」时生效：
// 仍有候选可切换时必须保持原有的「换个候选立刻重试」语义，否则会给上游额外放大
// 限流压力，也会让本来可以立刻自愈的请求白等一个退避窗口。
func TestShouldRetryThrottledCandidateOnlyForLastCandidate(t *testing.T) {
	ctxErr := error(nil)
	cases := []struct {
		name      string
		options   attemptOptions
		retried   bool
		status    int
		credID    uint64
		ctxErr    error
		wantRetry bool
	}{
		{"最后一个候选遇上 429", attemptOptions{throttleRetry: true}, false, http.StatusTooManyRequests, 22, ctxErr, true},
		{"还有候选可切换时不退避", attemptOptions{throttleRetry: false}, false, http.StatusTooManyRequests, 22, ctxErr, false},
		{"同一候选只退避一次", attemptOptions{throttleRetry: true}, true, http.StatusTooManyRequests, 22, ctxErr, false},
		{"非 429 失败不退避", attemptOptions{throttleRetry: true}, false, http.StatusInternalServerError, 22, ctxErr, false},
		{"没发出过真实尝试不退避", attemptOptions{throttleRetry: true}, false, http.StatusTooManyRequests, 0, ctxErr, false},
		{"客户端已断开不退避", attemptOptions{throttleRetry: true}, false, http.StatusTooManyRequests, 22, context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			last := channelAttempt{
				candidate: Candidate{ChannelCredentialID: tc.credID},
				result:    attemptResult{status: tc.status},
			}
			if got := shouldRetryThrottledCandidate(tc.options, tc.retried, last, tc.ctxErr); got != tc.wantRetry {
				t.Fatalf("shouldRetryThrottledCandidate = %v, want %v", got, tc.wantRetry)
			}
		})
	}
}

// 退避等待必须尊重请求上下文：客户端已经断开时立刻返回，不再占用退避窗口。
func TestWaitForThrottleRetryStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startedAt := time.Now()
	if waitForThrottleRetry(ctx) {
		t.Fatal("waitForThrottleRetry = true, want false for canceled context")
	}
	if elapsed := time.Since(startedAt); elapsed > candidateThrottleRetryDelay/2 {
		t.Fatalf("waitForThrottleRetry 在已取消的上下文上等待了 %v，应立即返回", elapsed)
	}
}

func TestAttemptFlowStepsIncludesPrecedingProtocolFallback(t *testing.T) {
	notFound := uint(http.StatusNotFound)
	result := attemptResult{
		status:           http.StatusOK,
		upstreamEndpoint: "/chat/completions",
		latency:          1200 * time.Millisecond,
		precedingFlow: []usage.AttemptFlowStep{{
			ChannelName: "ch-flow",
			Endpoint:    "/responses",
			DurationMs:  300,
			Status:      &notFound,
			Error:       "responses endpoint not supported",
		}},
	}
	steps := attemptFlowSteps("ch-flow", result)
	if len(steps) != 2 {
		t.Fatalf("steps = %d; want 2 (protocol fallback must count as a real upstream attempt)", len(steps))
	}
	if steps[0].Endpoint != "/responses" || steps[0].Status == nil || *steps[0].Status != http.StatusNotFound {
		t.Fatalf("first step must keep the failed primary hop: %+v", steps[0])
	}
	if steps[1].Endpoint != "/chat/completions" || steps[1].Status != nil {
		t.Fatalf("second step must be the successful final hop: %+v", steps[1])
	}
	// 没有协议回退时流程保持单步，尝试次数不受影响。
	plain := attemptResult{status: http.StatusOK, upstreamEndpoint: "/chat/completions", latency: time.Second}
	if steps := attemptFlowSteps("ch-flow", plain); len(steps) != 1 {
		t.Fatalf("plain attempt steps = %d; want 1", len(steps))
	}
}
