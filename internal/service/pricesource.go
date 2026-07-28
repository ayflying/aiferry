// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channel"
	. "github.com/yunloli/aiferry/internal/logic/pricesource"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type (
	IPriceSource interface {
		List(ctx context.Context) ([]View, error)
		Create(ctx context.Context, input adminapi.PriceSourceInput) (uint64, error)
		Update(ctx context.Context, id uint64, input adminapi.PriceSourceInput) error
		Delete(ctx context.Context, id uint64) error
		Sync(ctx context.Context, id uint64) (channel.PriceSyncResult, error)
		Get(ctx context.Context, id uint64) (entity.PriceSources, Config, error)
	}
)

var (
	localPriceSource IPriceSource
)

func PriceSource() IPriceSource {
	if localPriceSource == nil {
		panic("implement not found for interface IPriceSource, forgot register?")
	}
	return localPriceSource
}

func RegisterPriceSource(i IPriceSource) {
	localPriceSource = i
}
