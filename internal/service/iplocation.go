// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import _ "github.com/yunloli/aiferry/internal/logic/iplocation"

type (
	IIPLocation interface {
		Lookup(value string) string
	}
)

var (
	localIPLocation IIPLocation
)

func IPLocation() IIPLocation {
	if localIPLocation == nil {
		panic("implement not found for interface IIPLocation, forgot register?")
	}
	return localIPLocation
}

func RegisterIPLocation(i IIPLocation) {
	localIPLocation = i
}
