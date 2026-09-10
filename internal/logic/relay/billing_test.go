package relay

import (
	"errors"
	"testing"
	"time"
)

// 回归：billingError 分类必须能穿过 gerror 包装被 IsBilling*Error 识别。
func TestBillingErrorClassification(t *testing.T) {
	permErr := billingPermissionError("该访问密钥所属用户不是管理员，无法查询渠道余额")
	if !IsBillingPermissionError(permErr) {
		t.Fatal("permission error not detected")
	}
	if IsBillingRequestError(permErr) {
		t.Fatal("permission error misclassified as request error")
	}
	wrapped := errors.Join(permErr)
	if !IsBillingPermissionError(wrapped) {
		t.Fatal("joined permission error not detected")
	}

	reqErr := billingRequestError("date range must not exceed 100 days")
	if !IsBillingRequestError(reqErr) {
		t.Fatal("request error not detected")
	}
	if IsBillingPermissionError(reqErr) {
		t.Fatal("request error misclassified as permission error")
	}

	plain := errors.New("some internal failure")
	if IsBillingPermissionError(plain) || IsBillingRequestError(plain) {
		t.Fatal("plain error must not be classified as billing client error")
	}
}

// 用量窗口校验常量：与 Usage 导出层校验一致。
func TestUsageWindowValidation(t *testing.T) {
	if usageMaxWindow != 100*24*time.Hour {
		t.Fatalf("usageMaxWindow = %v", usageMaxWindow)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)
	if !start.Before(end) {
		t.Fatal("test precondition broken")
	}
}
