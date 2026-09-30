package user

import "testing"

func TestSetAdminRejectsSelfDemotionBeforeDatabase(t *testing.T) {
	service := &sUser{}
	err := service.SetAdmin(t.Context(), 7, 7, false)
	if err == nil || err.Error() != "不能移除自己的管理员权限" {
		t.Fatalf("SetAdmin() error = %v, want self-demotion rejection", err)
	}
}

func TestSetAdminRejectsZeroUser(t *testing.T) {
	service := &sUser{}
	err := service.SetAdmin(t.Context(), 0, 7, true)
	if err == nil || err.Error() != "用户不存在" {
		t.Fatalf("SetAdmin() error = %v, want missing user rejection", err)
	}
}

func TestNormalizeEmail(t *testing.T) {
	value, err := normalizeEmail("User@Example.COM")
	if err != nil || value != "user@example.com" {
		t.Fatalf("normalizeEmail() = %q, %v", value, err)
	}
	if value, err = normalizeEmail(" "); err != nil || value != "" {
		t.Fatalf("empty email should be accepted: %q, %v", value, err)
	}
	if _, err = normalizeEmail("not-an-email"); err == nil {
		t.Fatal("invalid email should be rejected")
	}
}
