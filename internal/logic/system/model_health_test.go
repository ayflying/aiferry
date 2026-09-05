package system

import (
	"testing"
	"time"
)

func TestModelHealthRelaySuccessByLatency(t *testing.T) {
	cases := []struct {
		name    string
		latency time.Duration
		want    int
	}{
		{name: "极快", latency: 500 * time.Millisecond, want: 5},
		{name: "较快", latency: 2 * time.Second, want: 4},
		{name: "正常", latency: 8 * time.Second, want: 3},
		{name: "较慢", latency: 20 * time.Second, want: 2},
		{name: "很慢", latency: 45 * time.Second, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ModelHealthRelaySuccessByLatency(tc.latency); got != tc.want {
				t.Fatalf("ModelHealthRelaySuccessByLatency(%s) = %d, want %d", tc.latency, got, tc.want)
			}
		})
	}
}

func TestIsAccountLevelFailure(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "余额不足", message: "Insufficient account balance", want: true},
		{name: "配额用尽", message: "You exceeded your current quota", want: true},
		{name: "组织停用", message: "This organization has been disabled.", want: true},
		{name: "令牌无效", message: "无效的令牌（请求头已经携带令牌）", want: true},
		{name: "无效API密钥", message: "Incorrect API key provided", want: true},
		{name: "余额不足英文变体", message: "Error: Insufficient balance, please recharge", want: true},
		{name: "配额耗尽变体", message: "quota exhausted for this token", want: true},
		{name: "额度已用尽", message: "该令牌额度已用尽，请充值后重试", want: true},
		{name: "欠费提示", message: "账户已欠费，请及时充值", want: true},
		{name: "普通模型错误", message: "model not found for this endpoint", want: false},
		{name: "上游超时", message: "context deadline exceeded", want: false},
		{name: "服务器错误", message: "500 Internal Server Error", want: false},
		{name: "空消息", message: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAccountLevelFailure(tc.message); got != tc.want {
				t.Fatalf("IsAccountLevelFailure(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}

func TestIsCredentialScopedFailure(t *testing.T) {
	cases := []struct {
		name  string
		input ModelDisableInput
		want  bool
	}{
		{name: "余额不足", input: ModelDisableInput{Message: "insufficient_quota"}, want: true},
		{name: "欠费变体", input: ModelDisableInput{Message: "该令牌额度已用尽"}, want: true},
		{name: "402状态码", input: ModelDisableInput{Status: 402, Message: "402 Payment Required"}, want: true},
		{name: "402未知文案", input: ModelDisableInput{Status: 402, Message: "upstream billing error"}, want: true},
		{name: "模型不存在", input: ModelDisableInput{Status: 404, Message: "model not found"}, want: false},
		{name: "限流", input: ModelDisableInput{Status: 429, Message: "rate limit exceeded"}, want: false},
		{name: "网关错误", input: ModelDisableInput{Status: 502, Message: "bad gateway"}, want: false},
		{name: "空输入", input: ModelDisableInput{}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCredentialScopedFailure(tc.input); got != tc.want {
				t.Fatalf("isCredentialScopedFailure(%+v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestModelDisableReason(t *testing.T) {
	reason := modelDisableReason(ModelDisableInput{Status: 502, Message: "bad gateway", TimedOut: false})
	if reason != "status_code=502, bad gateway" {
		t.Fatalf("unexpected reason: %s", reason)
	}
	reason = modelDisableReason(ModelDisableInput{Message: "context deadline exceeded", TimedOut: true})
	if reason != "timed_out=true, context deadline exceeded" {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

func TestMatchesAutoDisableSkipsHealthyModelScenario(t *testing.T) {
	// 确认普通失败在关闭自动禁用时完全不触发。
	settings := DefaultResilienceSettings()
	settings.AutoDisableEnabled = false
	input := AutoDisableInput{ChannelModelID: 1, Status: 502, Message: "bad gateway"}
	if IsAutoDisableMatch(settings, input) {
		t.Fatal("auto disable disabled should never match")
	}
}
