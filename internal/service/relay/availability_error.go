package relay

import (
	"errors"
	"net/http"

	"github.com/gogf/gf/v2/errors/gerror"
)

const (
	retryableAvailabilityMessage    = "All eligible channels are temporarily unavailable. Please retry shortly."
	retryableAvailabilityRetryAfter = "2"
)

var (
	ErrNoAvailableChannel        = gerror.New("no available channel")
	ErrEligibleChannelsExhausted = gerror.New("all eligible channels failed")
)

type ClientErrorResponse struct {
	Status     int
	Type       string
	Message    string
	RetryAfter string
}

func IsRetryableAvailabilityError(err error) bool {
	return errors.Is(err, ErrNoAvailableChannel) || errors.Is(err, ErrEligibleChannelsExhausted)
}

func RetryableAvailabilityClientError() ClientErrorResponse {
	return ClientErrorResponse{
		Status:     http.StatusServiceUnavailable,
		Type:       "server_error",
		Message:    retryableAvailabilityMessage,
		RetryAfter: retryableAvailabilityRetryAfter,
	}
}
