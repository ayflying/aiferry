// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
	_ "github.com/yunloli/aiferry/internal/logic/mail"
)

type (
	IMail interface {
		ClearChannelLowBalanceReminders(ctx context.Context, channelID uint64) error
		NotifyLowBalance(ctx context.Context, userID uint64)
		NotifyChannelLowBalance(ctx context.Context, channelID uint64, channelName string, remaining float64, currency string)
		SendTest(ctx context.Context, recipient string) error
	}
)

var (
	localMail IMail
)

func Mail() IMail {
	if localMail == nil {
		panic("implement not found for interface IMail, forgot register?")
	}
	return localMail
}

func RegisterMail(i IMail) {
	localMail = i
}
