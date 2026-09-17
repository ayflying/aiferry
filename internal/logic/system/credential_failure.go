package system

import "strings"

// definitiveCredentialFailure 仅对明确失效凭证立即禁用，不将泛化权限/限流文案升级为整把密钥禁用。
func definitiveCredentialFailure(input AutoDisableInput) bool {
	if input.Status == 402 {
		return true
	}
	message := strings.ToLower(input.Message)
	for _, word := range []string{"insufficient account balance", "insufficient balance", "insufficient_quota", "incorrect api key", "invalid api key", "余额不足", "余额耗尽", "账户已欠费", "账号已欠费", "套餐已到期", "套餐已过期"} {
		if strings.Contains(message, word) {
			return true
		}
	}
	return false
}
