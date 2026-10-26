package relay

import (
	"net/http"
	"testing"
)

func TestFreeUsageLimitDoesNotRetrySameCandidate(t *testing.T) {
	cases := []struct {
		name   string
		result attemptResult
		want   bool
	}{
		{"原始错误体", attemptResult{status: 429, body: []byte(`{"error":{"type":"FreeUsageLimitError","message":"Free usage limit reached"}}`)}, true},
		{"格式化文案", attemptResult{status: 429, errorMessage: `error: type="FreeUsageLimitError" message="Free usage limit reached"`}, true},
		{"文案存在仍检查原始错误体", attemptResult{status: 429, errorMessage: "upstream failed", body: []byte(`{"error":{"type":"FreeUsageLimitError"}}`)}, true},
		{"普通瞬时限流", attemptResult{status: 429, body: []byte(`{"error":{"type":"rate_limit_error"}}`)}, false},
		{"文案提及类型不算结构化错误", attemptResult{status: 429, body: []byte(`{"error":{"message":"FreeUsageLimitError"}}`)}, false},
		{"免费模型仍可瞬时限流", attemptResult{status: 429, errorMessage: `error: type="RateLimitError" message="Please retry later"`}, false},
		{"非429不受影响", attemptResult{status: 503, errorMessage: `error: type="FreeUsageLimitError"`}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isQuotaExhaustedResult(tc.result); got != tc.want {
				t.Fatalf("quota exhausted = %v, want %v", got, tc.want)
			}
			last := channelAttempt{candidate: Candidate{ChannelCredentialID: 22}, result: tc.result}
			delay, retry := sameCandidateRetryDelay(attemptOptions{throttleRetry: true}, 0, last, nil)
			wantRetry := tc.result.status == http.StatusTooManyRequests && !tc.want
			if retry != wantRetry {
				t.Fatalf("retry = %v, want %v", retry, wantRetry)
			}
			if tc.want && delay != 0 {
				t.Fatalf("quota exhausted must not wait: %v", delay)
			}
		})
	}
}
