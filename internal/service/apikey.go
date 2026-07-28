// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/apikey"
)

type (
	IAPIKey interface {
		Authenticate(ctx context.Context, bearer string) (AuthKey, error)
		CanUseModel(key AuthKey, model string) bool
		CanUseChannelGroups(key AuthKey, groupIDs []uint64) bool
		List(ctx context.Context) ([]View, error)
		Create(ctx context.Context, input adminapi.APIKeyInput) (Created, error)
		Reveal(ctx context.Context, id uint64) (string, error)
		Update(ctx context.Context, id uint64, input adminapi.APIKeyUpdate) error
		Delete(ctx context.Context, id uint64) error
		AddSpend(ctx context.Context, key AuthKey, amount float64) error
	}
)

var (
	localAPIKey IAPIKey
)

func APIKey() IAPIKey {
	if localAPIKey == nil {
		panic("implement not found for interface IAPIKey, forgot register?")
	}
	return localAPIKey
}

func RegisterAPIKey(i IAPIKey) {
	localAPIKey = i
}
