// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelCredentialsDao is the data access object for the table channel_credentials.
type ChannelCredentialsDao struct {
	table    string                    // table is the underlying table name of the DAO.
	group    string                    // group is the database configuration group name of the current DAO.
	columns  ChannelCredentialsColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler        // handlers for customized model modification.
}

// ChannelCredentialsColumns defines and stores column names for the table channel_credentials.
type ChannelCredentialsColumns struct {
	Id                     string //
	ChannelId              string //
	KeyPrefix              string //
	KeyHash                string //
	ApiKeyCipher           string //
	Status                 string //
	AutoDisabledAt         string //
	AutoDisabledReason     string //
	AutoDisabledStatusCode string //
	AutoDisabledSource     string //
	LastCostUsed           string //
	LastCostRemaining      string //
	LastCostCurrency       string //
	LastCostAt             string //
	CreatedAt              string //
	UpdatedAt              string //
	DeletedAt              string //
}

// channelCredentialsColumns holds the columns for the table channel_credentials.
var channelCredentialsColumns = ChannelCredentialsColumns{
	Id:                     "id",
	ChannelId:              "channel_id",
	KeyPrefix:              "key_prefix",
	KeyHash:                "key_hash",
	ApiKeyCipher:           "api_key_cipher",
	Status:                 "status",
	AutoDisabledAt:         "auto_disabled_at",
	AutoDisabledReason:     "auto_disabled_reason",
	AutoDisabledStatusCode: "auto_disabled_status_code",
	AutoDisabledSource:     "auto_disabled_source",
	LastCostUsed:           "last_cost_used",
	LastCostRemaining:      "last_cost_remaining",
	LastCostCurrency:       "last_cost_currency",
	LastCostAt:             "last_cost_at",
	CreatedAt:              "created_at",
	UpdatedAt:              "updated_at",
	DeletedAt:              "deleted_at",
}

// NewChannelCredentialsDao creates and returns a new DAO object for table data access.
func NewChannelCredentialsDao(handlers ...gdb.ModelHandler) *ChannelCredentialsDao {
	return &ChannelCredentialsDao{
		group:    "default",
		table:    "channel_credentials",
		columns:  channelCredentialsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ChannelCredentialsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ChannelCredentialsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ChannelCredentialsDao) Columns() ChannelCredentialsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ChannelCredentialsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ChannelCredentialsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ChannelCredentialsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
