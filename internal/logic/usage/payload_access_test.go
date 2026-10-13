package usage

import (
	"errors"
	"testing"
)

func TestPayloadAccessOwnership(t *testing.T) {
	lookup := func(id string, owner uint64) (bool, error) {
		return id == "own-request" && (owner == 42 || owner == 0), nil
	}
	tests := []struct {
		name, id       string
		user           uint64
		admin, allowed bool
	}{
		{"本人调用", "own-request", 42, false, true},
		{"其他用户不可越权", "own-request", 43, false, false},
		{"管理员可查看其他人", "own-request", 43, true, true},
		{"不存在", "missing", 42, false, false},
		{"管理员不能查看不存在记录", "missing", 43, true, false},
		{"未登录不能借管理员标记越权", "own-request", 0, true, false},
		{"空ID", " ", 42, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkPayloadAccess(test.id, test.user, test.admin, lookup)
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v err=%v", test.allowed, err)
			}
		})
	}
}

func TestPayloadAccessFailsClosedOnLookupError(t *testing.T) {
	err := checkPayloadAccess("own-request", 42, false, func(string, uint64) (bool, error) { return true, errors.New("lookup failed") })
	if err == nil {
		t.Fatal("数据库错误必须拒绝访问")
	}
}
