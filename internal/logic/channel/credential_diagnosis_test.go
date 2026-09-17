package channel

import "testing"

// 「选凭证阶段被跳过」的文案必须与实际过滤条件一致。历史实现按渠道级重查，
// 把「模型 × 密钥」组合冷却写成「有可用密钥」，整条文案自相矛盾并带偏排查。
func TestCredentialUnavailableReasonText(t *testing.T) {
	cases := []struct {
		name               string
		afterChannelFilter int
		excludedCount      int
		channelReason      string
		want               string
	}{
		{
			name:               "组合冷却",
			afterChannelFilter: 3,
			want:               "3 把密钥在该模型下全部处于组合冷却",
		},
		{
			name:               "组合冷却优先于排除集合",
			afterChannelFilter: 1,
			excludedCount:      2,
			channelReason:      credentialSkipReasonAvailable,
			want:               "1 把密钥在该模型下全部处于组合冷却",
		},
		{
			name:          "渠道无启用密钥",
			channelReason: "无启用密钥",
			want:          "无启用密钥",
		},
		{
			name:          "渠道级凭证全部冷却",
			channelReason: "2 把密钥全部冷却中",
			want:          "2 把密钥全部冷却中",
		},
		{
			name:          "本次请求排除全部密钥",
			excludedCount: 2,
			channelReason: credentialSkipReasonAvailable,
			want:          "本次请求已排除该渠道全部可选密钥",
		},
		{
			name:          "有可用密钥且无排除",
			channelReason: credentialSkipReasonAvailable,
			want:          credentialSkipReasonAvailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := credentialUnavailableReasonText(tc.afterChannelFilter, tc.excludedCount, tc.channelReason); got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}
