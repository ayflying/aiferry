package relay

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestTransientRetryPolicy(t *testing.T) {
	overload := attemptResult{status: http.StatusBadGateway, errorMessage: `error: type="service_unavailable_error" param=null code="server_error" message="Our servers are currently overloaded. Please try again later."`}
	tests := []struct {
		name    string
		result  attemptResult
		retries int
		enabled bool
		ctxErr  error
		want    time.Duration
	}{
		{"截图过载首次重试", overload, 0, true, nil, 1500 * time.Millisecond},
		{"过载第二次重试", overload, 1, true, nil, 5 * time.Second},
		{"过载第三次重试", overload, 2, true, nil, 10 * time.Second},
		{"过载重试耗尽", overload, 3, true, nil, 0},
		{"优先切换其它候选", overload, 0, false, nil, 0},
		{"取消后不重试", overload, 0, true, context.Canceled, 0},
		{"超时后不重试", overload, 0, true, context.DeadlineExceeded, 0},
		{"已输出不重放", attemptResult{status: 502, wroteBytes: true}, 0, true, nil, 0},
		{"下游写失败不重放", attemptResult{status: 502, writerFailed: true}, 0, true, nil, 0},
		{"服务内部暂时失败", attemptResult{status: 500}, 0, true, nil, 1500 * time.Millisecond},
		{"服务不可用", attemptResult{status: 503}, 0, true, nil, 1500 * time.Millisecond},
		{"网关超时", attemptResult{status: 504}, 0, true, nil, 1500 * time.Millisecond},
		{"未实现不重试", attemptResult{status: 501}, 0, true, nil, 0},
		{"协议错误不重试", attemptResult{status: 505}, 0, true, nil, 0},
		{"参数错误不重试", attemptResult{status: 400}, 0, true, nil, 0},
		{"鉴权错误不重试", attemptResult{status: 401}, 0, true, nil, 0},
		{"免费额度耗尽不重试", attemptResult{status: 429, errorMessage: `type="FreeUsageLimitError"`}, 0, true, nil, 0},
		{"正常流结束不重放", attemptResult{status: 502, streamCompleted: true}, 0, true, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			last := channelAttempt{candidate: Candidate{ChannelCredentialID: 22}, result: tt.result}
			delay, retry := sameCandidateRetryDelay(attemptOptions{transientRetry: tt.enabled}, tt.retries, last, tt.ctxErr)
			if delay != tt.want || retry != (tt.want > 0) {
				t.Fatalf("退避 = %v, %v；预期 %v", delay, retry, tt.want)
			}
		})
	}
}

func TestTransientRetryHonorsDisabledChannel(t *testing.T) {
	last := channelAttempt{candidate: Candidate{ChannelCredentialID: 22}, result: attemptResult{status: 502}, channelDead: true}
	if _, retry := sameCandidateRetryDelay(attemptOptions{transientRetry: true}, 0, last, nil); retry {
		t.Fatal("显式禁用的渠道不能被暂时故障兜底重新启用")
	}
	last.channelDead = false
	last.candidate.ChannelCredentialID = 0
	if _, retry := sameCandidateRetryDelay(attemptOptions{transientRetry: true}, 0, last, nil); retry {
		t.Fatal("未取得密钥不能凭空退避重试")
	}
}

// 真实 SSE 错误解析后的 502 必须进入相同策略，而不是依赖原始 HTTP 状态。
func TestStreamOverloadRetryBeforeOutput(t *testing.T) {
	failure, ok := parseStreamFailure([]byte("data: {\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"server_error\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}\n"))
	if !ok || failure.status != http.StatusBadGateway {
		t.Fatalf("过载解析失败：%+v, %v", failure, ok)
	}
	result := attemptResult{status: failure.status, body: failure.body, errorMessage: failure.message}
	if attemptCompleted(result, nil, true) {
		t.Fatal("无输出的过载错误不能终结重试链")
	}
	last := channelAttempt{candidate: Candidate{ChannelCredentialID: 22}, result: result}
	if _, retry := sameCandidateRetryDelay(attemptOptions{transientRetry: true}, 0, last, nil); !retry {
		t.Fatal("流式过载错误应可重试")
	}
}
