package channel

import (
	"testing"
	"time"
)

func TestCanRestoreCostQueryDisabledCredential(t *testing.T) {
	disabledAt := time.Date(2026, time.July, 21, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		credential costQueryDisabledCredential
		want       bool
	}{
		{name: "费用查询停用的密钥恢复", credential: costQueryDisabledCredential{Status: 0, AutoDisabledAt: disabledAt, AutoDisabledSource: costQueryAutoDisableSource}, want: true},
		{name: "人工停用不恢复", credential: costQueryDisabledCredential{Status: 0, AutoDisabledSource: costQueryAutoDisableSource}, want: false},
		{name: "请求失败停用不恢复", credential: costQueryDisabledCredential{Status: 0, AutoDisabledAt: disabledAt, AutoDisabledSource: "relay_request"}, want: false},
		{name: "已启用的密钥不处理", credential: costQueryDisabledCredential{Status: 1, AutoDisabledAt: disabledAt, AutoDisabledSource: costQueryAutoDisableSource}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canRestoreCostQueryDisabledCredential(test.credential); got != test.want {
				t.Fatalf("canRestoreCostQueryDisabledCredential() = %t, want %t", got, test.want)
			}
		})
	}
}
