package channel

import "strings"

// 旧的密封凭据仍可能存在于数据库中；不得将其当作普通 API key 显示或转发。
func isRetiredCredential(value string) bool {
	return strings.HasPrefix(value, "aiferry:gemini-oauth:v1:")
}
