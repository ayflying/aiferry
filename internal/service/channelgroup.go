// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"

	adminapi "github.com/yunloli/aiferry/api/admin"
	. "github.com/yunloli/aiferry/internal/logic/channelgroup"
)

type (
	IChannelGroup interface {
		List(ctx context.Context) ([]View, error)
		Create(ctx context.Context, input adminapi.ChannelGroupInput) (uint64, error)
		Update(ctx context.Context, id uint64, input adminapi.ChannelGroupInput) error
		Delete(ctx context.Context, id uint64) error
		ChannelIDs(ctx context.Context, channelID uint64) ([]uint64, error)
		SetChannelIDs(ctx context.Context, channelID uint64, groupIDs []uint64) error
	}
)

var (
	localChannelGroup IChannelGroup
)

func ChannelGroup() IChannelGroup {
	if localChannelGroup == nil {
		panic("implement not found for interface IChannelGroup, forgot register?")
	}
	return localChannelGroup
}

func RegisterChannelGroup(i IChannelGroup) {
	localChannelGroup = i
}
