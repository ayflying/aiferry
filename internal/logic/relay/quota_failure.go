package relay

import (
	"encoding/json"
	"net/http"
	"strings"
)

// isQuotaExhaustedResult 区分免费额度耗尽与突发限流。OpenCode 的
// FreeUsageLimitError 没有承诺短期恢复，不能用 1.5 秒退避反复消耗上游请求。
// 只识别结构化错误类型，不把普通提示词中的「limit」误判成额度耗尽；
// 不禁用整把密钥，因为同一密钥的付费模型可能仍可用。
func isQuotaExhaustedResult(result attemptResult) bool {
	if result.status != http.StatusTooManyRequests {
		return false
	}
	var envelope struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(result.body, &envelope) == nil && envelope.Error.Type == "FreeUsageLimitError" {
		return true
	}
	// 流式错误或协议转换后可能只保留 upstreamerror 生成的格式化文案。
	return strings.Contains(result.errorMessage, `type="FreeUsageLimitError"`)
}
