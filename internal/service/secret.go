// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import _ "github.com/yunloli/aiferry/internal/logic/secret"

type (
	ISecret interface {
		Encrypt(plainText string) (string, error)
		Decrypt(value string) (string, error)
	}
)

var (
	localSecret ISecret
)

func Secret() ISecret {
	if localSecret == nil {
		panic("implement not found for interface ISecret, forgot register?")
	}
	return localSecret
}

func RegisterSecret(i ISecret) {
	localSecret = i
}
