// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/yunloli/aiferry/internal/logic/usage"
	. "github.com/yunloli/aiferry/internal/logic/user"
)

type (
	IUser interface {
		Profile(ctx context.Context, id uint64) (Profile, error)
		UpdateProfile(ctx context.Context, id uint64, nickname string, email string) (Profile, error)
		Usage(ctx context.Context, id uint64, days int) (usage.UserSummary, error)
		List(ctx context.Context) ([]ManagedUser, error)
		ListOptions(ctx context.Context) ([]Option, error)
		AdminEmails(ctx context.Context) ([]string, error)
		UpdateBalance(ctx context.Context, id uint64, balance float64) (Profile, error)
		CheckBalance(ctx context.Context, id uint64) error
		Debit(ctx context.Context, id uint64, amount decimal.Decimal) error
		Credit(ctx context.Context, id uint64, amount decimal.Decimal) error
		Delete(ctx context.Context, id uint64, operatorID uint64) error
	}
)

var (
	localUser IUser
)

func User() IUser {
	if localUser == nil {
		panic("implement not found for interface IUser, forgot register?")
	}
	return localUser
}

func RegisterUser(i IUser) {
	localUser = i
}
