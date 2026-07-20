package relay

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/service/usage"
)

const maxFailureLogLength = 960

func detailedFailureLog(result attemptResult, recordedStatus int, reason string, stream bool, attempts int, durationMs int64) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	lines := []string{"失败原因：" + redactFailureText(reason)}
	if recordedStatus > 0 {
		lines = append(lines, "记录状态："+formatFailureStatus(recordedStatus))
	}
	if result.status > 0 && result.status != recordedStatus {
		lines = append(lines, "上游状态："+formatFailureStatus(result.status))
	}
	if result.upstreamEndpoint != "" {
		lines = append(lines, "上游接口："+result.upstreamEndpoint)
	}
	if result.protocolConversion != "" {
		lines = append(lines, "协议转换："+result.protocolConversion)
	}
	if stream {
		lines = append(lines, "调用方式：流式")
	} else {
		lines = append(lines, "调用方式：非流式")
	}
	if attempts > 0 {
		lines = append(lines, fmt.Sprintf("上游尝试：%d 次", attempts))
	}
	lines = append(lines, "总耗时："+formatFailureDuration(durationMs))
	if result.firstTokenMs != nil {
		lines = append(lines, "首包耗时："+formatFailureDuration(*result.firstTokenMs))
	}
	if result.timedOut {
		lines = append(lines, "超时：是")
	}
	if upstreamMessage := strings.TrimSpace(result.errorMessage); upstreamMessage != "" && upstreamMessage != reason {
		lines = append(lines, "上游错误信息："+redactFailureText(upstreamMessage))
	}
	lines = append(lines, "解析用量："+formatFailureUsage(result.tokens))
	if result.status < http.StatusOK || result.status >= http.StatusMultipleChoices {
		lines = append(lines, upstreamErrorDetails(result.body, reason)...)
	}
	return truncateFailureLog(strings.Join(lines, "\n"), maxFailureLogLength)
}

func formatFailureStatus(status int) string {
	if text := http.StatusText(status); text != "" {
		return fmt.Sprintf("HTTP %d %s", status, text)
	}
	return fmt.Sprintf("HTTP %d", status)
}

func formatFailureDuration(milliseconds int64) string {
	if milliseconds <= 0 {
		return "0s"
	}
	return (time.Duration(milliseconds) * time.Millisecond).Round(time.Millisecond).String()
}

func formatFailureUsage(tokens usage.TokenUsage) string {
	return fmt.Sprintf("输入=%s，缓存=%s，输出=%s，总计=%s",
		formatFailureTokenCount(tokens.Input),
		formatFailureTokenCount(tokens.CachedInput),
		formatFailureTokenCount(tokens.Output),
		formatFailureTokenCount(tokens.Total),
	)
}

func formatFailureTokenCount(value *uint64) string {
	if value == nil {
		return "未返回"
	}
	return fmt.Sprintf("%d", *value)
}

func upstreamErrorDetails(body []byte, reason string) []string {
	var lines []string
	if kind := strings.TrimSpace(gjson.GetBytes(body, "error.type").String()); kind != "" {
		lines = append(lines, "上游错误类型："+redactFailureText(kind))
	}
	if code := strings.TrimSpace(gjson.GetBytes(body, "error.code").String()); code != "" {
		lines = append(lines, "上游错误代码："+redactFailureText(code))
	}
	if message := strings.TrimSpace(gjson.GetBytes(body, "error.message").String()); message != "" && message != reason {
		lines = append(lines, "上游错误信息："+redactFailureText(message))
	}
	return lines
}

func redactFailureText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	for _, secret := range []string{"Bearer ", "bearer ", "sk-"} {
		if index := strings.Index(value, secret); index >= 0 {
			value = value[:index] + secret + "***"
		}
	}
	return value
}

func truncateFailureLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	var builder strings.Builder
	for _, runeValue := range value {
		if builder.Len()+len(string(runeValue))+3 > limit {
			break
		}
		builder.WriteRune(runeValue)
	}
	return builder.String() + "..."
}
