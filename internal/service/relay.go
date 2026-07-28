// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
	"net/http"

	"github.com/yunloli/aiferry/internal/logic/apikey"
	. "github.com/yunloli/aiferry/internal/logic/relay"
)

type (
	IRelay interface {
		Models(ctx context.Context, key apikey.AuthKey) (ModelList, error)
		Handle(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, clientIP string, endpoint string, body []byte, key apikey.AuthKey) error
	}
)

var (
	localRelay IRelay
)

func Relay() IRelay {
	if localRelay == nil {
		panic("implement not found for interface IRelay, forgot register?")
	}
	return localRelay
}

func RegisterRelay(i IRelay) {
	localRelay = i
}
