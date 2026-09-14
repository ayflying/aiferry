package relay

import (
	"errors"
	"net/http"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	retryableAvailabilityMessage    = "All eligible channels are temporarily unavailable. Please retry shortly."
	retryableAvailabilityRetryAfter = "2"

	concurrencyExhaustedMessage    = "Channel upstream key concurrency limit reached. Please retry shortly."
	concurrencyExhaustedRetryAfter = "5"
)

var (
	ErrNoAvailableChannel        = gerror.New("no available channel")
	ErrEligibleChannelsExhausted = gerror.New("all eligible channels failed")
	// ErrChannelConcurrencyExhausted 表示渠道全部密钥的并发额度都已占满，且在等待窗口内
	// 没有腾出空位。它是网关本地判定，不代表上游故障，因此不进入渠道自动禁用评分
	// （默认禁用状态码包含 429，若把它当成转发结果会把健康渠道打成连续失败），
	// 直接以 429 让客户端稍后重试。
	ErrChannelConcurrencyExhausted = gerror.New("channel key concurrency exhausted")
)

func IsRetryableAvailabilityError(err error) bool {
	return errors.Is(err, ErrNoAvailableChannel) || errors.Is(err, ErrEligibleChannelsExhausted)
}

// ClientErrorResponse 描述由网关本地判定、需要直接回给客户端的错误响应。
type ClientErrorResponse struct {
	Status     int
	Type       string
	Message    string
	RetryAfter string
}

func IsConcurrencyExhaustedError(err error) bool {
	return errors.Is(err, ErrChannelConcurrencyExhausted)
}

func RetryableAvailabilityClientError() ClientErrorResponse {
	return ClientErrorResponse{
		Status:     http.StatusServiceUnavailable,
		Type:       "server_error",
		Message:    retryableAvailabilityMessage,
		RetryAfter: retryableAvailabilityRetryAfter,
	}
}

func ConcurrencyExhaustedClientError() ClientErrorResponse {
	return ClientErrorResponse{
		Status:     http.StatusTooManyRequests,
		Type:       "rate_limit_exceeded",
		Message:    concurrencyExhaustedMessage,
		RetryAfter: concurrencyExhaustedRetryAfter,
	}
}
