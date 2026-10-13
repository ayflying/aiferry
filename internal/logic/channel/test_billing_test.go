package channel

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/usage"
)

func TestTestChargesUser(t *testing.T) {
	tests := []struct {
		name              string
		userID, creatorID uint64
		want              bool
	}{
		{"自有渠道不扣余额", 42, 42, false},
		{"他人渠道仍扣余额", 42, 43, true},
		{"无归属渠道不豁免", 42, 0, true},
		{"系统巡检不扣余额", usage.SystemUserID, 42, false},
		{"系统自有渠道不扣余额", usage.SystemUserID, usage.SystemUserID, false},
		{"零创建者不能视为自有渠道", 0, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := testChargesUser(test.userID, test.creatorID); got != test.want {
				t.Fatalf("testChargesUser(%d, %d) = %v, want %v", test.userID, test.creatorID, got, test.want)
			}
		})
	}
}
