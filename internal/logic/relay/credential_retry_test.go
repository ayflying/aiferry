package relay

import (
	"net/http"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

// 上游按有状态会话校验拒绝本会话的工具调用历史时，必须放行到下一个候选渠道：
// 这类失败的根源在渠道侧（它只认自己生成过的 tool_call 记录），实测换到无状态上游
// 全部成功；若在这里当成对客户端请求的最终判决，就会把渠道问题暴露成用户的请求错误。
func TestNonRetryableClientFailureAllowsStatefulSessionRejection(t *testing.T) {
	// 故意不把 400 放进 RetryStatusCodes：放行依据是「上游归因」，不是管理端配置。
	settings := adminapi.SystemResilienceSettingsInput{RetryStatusCodes: "401,403,404,408,429,500-599"}
	cases := map[string][]byte{
		"deepseek": []byte(`{"error":{"type":"invalid_request_error","message":"Error from provider (Console Go): Upstream request failed: [invalid_request_error] The reasoning_content in the thinking mode must be passed back to the API."}}`),
		"kimi":     []byte(`{"error":{"message":"thinking is enabled but reasoning_content is missing in assistant tool call message at index 2"}}`),
	}
	for name, body := range cases {
		for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
			result := attemptResult{status: status, body: body}
			if nonRetryableClientFailure(result, nil, settings) {
				t.Fatalf("%s status %d: stateful session rejection must keep routing to the next candidate", name, status)
			}
		}
	}
}

// 上游只把错误留在 errorMessage 里（响应体缺失或不可解析）时同样要认得出来。
func TestUpstreamStatefulSessionRejectionReadsErrorMessage(t *testing.T) {
	message := "The `reasoning_content` in the thinking mode must be passed back to the API."
	if !upstreamStatefulSessionRejection(http.StatusBadRequest, nil, message) {
		t.Fatal("must detect the marker in errorMessage when the body is empty")
	}
	if !upstreamStatefulSessionRejection(http.StatusBadRequest, []byte("reasoning_content is required"), "") {
		t.Fatal("must fall back to the raw body when error.message is absent")
	}
}

// 普通 400（请求体本身被上游拒绝）仍然是最终判决：这次修复只放行「上游归因于会话状态」
// 的那一类，否则会把真正的客户端请求错误放大到每一个候选渠道。
func TestNonRetryableClientFailureKeepsPlainBadRequestTerminal(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{RetryStatusCodes: "400-407,500-599"}
	plain := attemptResult{status: http.StatusBadRequest, body: []byte(`{"error":{"message":"messages: at least one message is required"}}`)}
	if !nonRetryableClientFailure(plain, nil, settings) {
		t.Fatal("a plain 400 must stay terminal even when 400 is listed as retryable")
	}
	// 已向客户端写出内容的失败无法重放：由 attemptCompleted 直接判定为「本次尝试到此结束」，
	// 根本不会进入换候选决策，因此这类失败也不会被会话状态判定放行。
	written := attemptResult{status: http.StatusBadRequest, wroteBytes: true, body: []byte(`reasoning_content must be passed back`)}
	if !attemptCompleted(written, nil, true) {
		t.Fatal("a failure after bytes were written must end the attempt")
	}
}

// 只有 400/422 才可能命中会话状态判定，其它状态码保持原有重试语义。
func TestUpstreamStatefulSessionRejectionIgnoresOtherStatuses(t *testing.T) {
	body := []byte(`{"error":{"message":"reasoning_content must be passed back"}}`)
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		if upstreamStatefulSessionRejection(status, body, "") {
			t.Fatalf("status %d must not match the stateful session rejection", status)
		}
	}
}
