// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/redemption"
)

type (
	IRedemption interface {
		Redeem(ctx context.Context, userID uint64, input adminapi.RedemptionCodeRedeemInput) (RedeemResult, error)
		Create(ctx context.Context, operatorID uint64, input adminapi.RedemptionCodeCreateInput) ([]CreatedCode, error)
		List(ctx context.Context, filter ListFilter) ([]View, error)
		DeleteInvalid(ctx context.Context) (int, error)
	}
)

var (
	localRedemption IRedemption
)

func Redemption() IRedemption {
	if localRedemption == nil {
		panic("implement not found for interface IRedemption, forgot register?")
	}
	return localRedemption
}

func RegisterRedemption(i IRedemption) {
	localRedemption = i
}
