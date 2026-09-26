package auth

import (
	"testing"
	"time"
)

// 产品约定：登录会话写死 30 天并按请求滑动续期，不接受 SESSION_TTL_HOURS 之类的环境变量覆盖。
// 之前默认 7 天、部署配置又被压到十几小时，导致隔夜就掉登录。
func TestSessionTTLIsHardcodedThirtyDays(t *testing.T) {
	t.Setenv("SESSION_TTL_HOURS", "12")

	s := &sAuth{}
	got := s.sessionTTL()
	const want = 30 * 24 * time.Hour
	if got != want {
		t.Fatalf("sessionTTL() = %s, want %s", got, want)
	}
}
