package relay

import (
	"strings"
	"testing"

	"github.com/yunloli/aiferry/internal/service/usage"
)

func TestDetailedFailureLogExplainsMissingStreamUsage(t *testing.T) {
	firstToken := int64(8400)
	detail := detailedFailureLog(attemptResult{
		status:           200,
		firstTokenMs:     &firstToken,
		upstreamEndpoint: "/chat/completions",
	}, 402, "上游响应未返回可计费的用量信息", true, 1, 12300)
	for _, expected := range []string{
		"失败原因：上游响应未返回可计费的用量信息",
		"记录状态：HTTP 402 Payment Required",
		"上游状态：HTTP 200 OK",
		"上游接口：/chat/completions",
		"调用方式：流式",
		"上游尝试：1 次",
		"总耗时：12.3s",
		"首包耗时：8.4s",
		"解析用量：输入=未返回，缓存=未返回，输出=未返回，总计=未返回",
	} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("failure detail does not contain %q: %s", expected, detail)
		}
	}
}

func TestDetailedFailureLogIncludesSafeUpstreamErrorMetadata(t *testing.T) {
	detail := detailedFailureLog(attemptResult{
		status: 402,
		body:   []byte(`{"error":{"message":"account balance is empty","type":"insufficient_quota","code":"balance_empty"}}`),
	}, 402, "account balance is empty", false, 2, 2300)
	for _, expected := range []string{"上游错误类型：insufficient_quota", "上游错误代码：balance_empty"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("failure detail does not contain %q: %s", expected, detail)
		}
	}
}

func TestDetailedFailureLogKeepsEmbeddedStreamPaymentFailure(t *testing.T) {
	reason := `error: code=402 type="insufficient_quota" message="upstream balance is empty"`
	detail := detailedFailureLog(attemptResult{
		status:       402,
		body:         []byte(`{"error":{"code":402,"type":"insufficient_quota","message":"upstream balance is empty"}}`),
		errorMessage: reason,
	}, 402, reason, true, 3, 49032)
	for _, expected := range []string{"失败原因：" + reason, "上游错误类型：insufficient_quota", "上游错误信息：upstream balance is empty", "上游尝试：3 次"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("failure detail does not contain %q: %s", expected, detail)
		}
	}
	if strings.Contains(detail, "上游状态：HTTP 200") {
		t.Fatalf("embedded failure must not be recorded as an upstream 200: %s", detail)
	}
}

func TestDetailedFailureLogKeepsStreamFailureAlongsideBillingFailure(t *testing.T) {
	detail := detailedFailureLog(attemptResult{status: 200, errorMessage: "upstream stream idle timeout", timedOut: true}, 402, "上游响应未返回可计费的用量信息", true, 1, 9000)
	for _, expected := range []string{"超时：是", "上游错误信息：upstream stream idle timeout"} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("failure detail does not contain %q: %s", expected, detail)
		}
	}
}

func TestFormatFailureUsage(t *testing.T) {
	input := uint64(12)
	if got := formatFailureUsage(usage.TokenUsage{Input: &input}); got != "输入=12，缓存=未返回，输出=未返回，总计=未返回" {
		t.Fatalf("unexpected usage detail: %s", got)
	}
}
