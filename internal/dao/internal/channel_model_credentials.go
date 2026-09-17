// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelModelCredentialsDao is the data access object for the table channel_model_credentials.
type ChannelModelCredentialsDao struct {
	table    string                         // table is the underlying table name of the DAO.
	group    string                         // group is the database configuration group name of the current DAO.
	columns  ChannelModelCredentialsColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler             // handlers for customized model modification.
}

// ChannelModelCredentialsColumns defines and stores column names for the table channel_model_credentials.
type ChannelModelCredentialsColumns struct {
	Id                  string //
	ChannelId           string //
	ChannelModelId      string //
	ChannelCredentialId string //
	HealthScore         string // 组合健康分（0-100），默认100
	CooldownUntil       string // 冷却截至时间，NULL 表示未冷却
	LastError           string // 最近一次错误摘要
	CreatedAt           string //
	UpdatedAt           string //
}

// channelModelCredentialsColumns holds the columns for the table channel_model_credentials.
var channelModelCredentialsColumns = ChannelModelCredentialsColumns{
	Id:                  "id",
	ChannelId:           "channel_id",
	ChannelModelId:      "channel_model_id",
	ChannelCredentialId: "channel_credential_id",
	HealthScore:         "health_score",
	CooldownUntil:       "cooldown_until",
	LastError:           "last_error",
	CreatedAt:           "created_at",
	UpdatedAt:           "updated_at",
}

// NewChannelModelCredentialsDao creates and returns a new DAO object for table data access.
func NewChannelModelCredentialsDao(handlers ...gdb.ModelHandler) *ChannelModelCredentialsDao {
	return &ChannelModelCredentialsDao{
		group:    "default",
		table:    "channel_model_credentials",
		columns:  channelModelCredentialsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ChannelModelCredentialsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ChannelModelCredentialsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ChannelModelCredentialsDao) Columns() ChannelModelCredentialsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ChannelModelCredentialsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ChannelModelCredentialsDao) Ctx(ctx context.Context) *gdb.Model {
	model := dao.DB().Model(dao.table)
	for _, handler := range dao.handlers {
		model = handler(model)
	}
	return model.Safe().Ctx(ctx)
}

// Transaction wraps the transaction logic using function f.
// It rolls back the transaction and returns the error if function f returns a non-nil error.
// It commits the transaction and returns nil if function f returns nil.
//
// Note: Do not commit or roll back the transaction in function f,
// as it is automatically handled by this function.
func (dao *ChannelModelCredentialsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
