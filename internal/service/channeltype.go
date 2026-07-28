// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type (
	IChannelType interface {
		List(ctx context.Context) ([]View, error)
		Get(ctx context.Context, id uint64) (entity.ChannelTypes, Config, error)
		GetByCode(ctx context.Context, code string) (entity.ChannelTypes, Config, error)
		Create(ctx context.Context, input adminapi.ChannelTypeInput) (uint64, error)
		Update(ctx context.Context, id uint64, input adminapi.ChannelTypeInput) error
		SetStatus(ctx context.Context, id uint64, status int) error
		Delete(ctx context.Context, id uint64) error
	}
)

var (
	localChannelType IChannelType
)

func ChannelType() IChannelType {
	if localChannelType == nil {
		panic("implement not found for interface IChannelType, forgot register?")
	}
	return localChannelType
}

func RegisterChannelType(i IChannelType) {
	localChannelType = i
}
