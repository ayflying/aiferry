// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/requestfirewall"
)

type (
	IRequestFirewall interface {
		SetSettings(settings adminapi.RequestFirewallSettingsInput)
		Acquire(ctx context.Context, input RequestInput) (func(), *Rejection)
		// AcquireClient admits a request before API key authentication. This makes the
		// global and per-IP limits effective for malformed and invalid-key requests.
		AcquireClient(ctx context.Context, input RequestInput) (func(), *Rejection)
		// AcquireAPIKey applies the API-key-specific limits after authentication.
		// The caller must already hold a client admission while this release function
		// remains active.
		AcquireAPIKey(input RequestInput) (func(), *Rejection)
	}
)

var (
	localRequestFirewall IRequestFirewall
)

func RequestFirewall() IRequestFirewall {
	if localRequestFirewall == nil {
		panic("implement not found for interface IRequestFirewall, forgot register?")
	}
	return localRequestFirewall
}

func RegisterRequestFirewall(i IRequestFirewall) {
	localRequestFirewall = i
}
