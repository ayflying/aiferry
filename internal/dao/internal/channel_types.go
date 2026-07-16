// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelTypesDao is the data access object for the table channel_types.
type ChannelTypesDao struct {
	table    string              // table is the underlying table name of the DAO.
	group    string              // group is the database configuration group name of the current DAO.
	columns  ChannelTypesColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler  // handlers for customized model modification.
}

// ChannelTypesColumns defines and stores column names for the table channel_types.
type ChannelTypesColumns struct {
	Id         string //
	Name       string //
	Code       string //
	ConfigJson string //
	Status     string //
	BuiltIn    string //
	CreatedAt  string //
	UpdatedAt  string //
	DeletedAt  string //
}

// channelTypesColumns holds the columns for the table channel_types.
var channelTypesColumns = ChannelTypesColumns{
	Id:         "id",
	Name:       "name",
	Code:       "code",
	ConfigJson: "config_json",
	Status:     "status",
	BuiltIn:    "built_in",
	CreatedAt:  "created_at",
	UpdatedAt:  "updated_at",
	DeletedAt:  "deleted_at",
}

// NewChannelTypesDao creates and returns a new DAO object for table data access.
func NewChannelTypesDao(handlers ...gdb.ModelHandler) *ChannelTypesDao {
	return &ChannelTypesDao{
		group:    "default",
		table:    "channel_types",
		columns:  channelTypesColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ChannelTypesDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ChannelTypesDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ChannelTypesDao) Columns() ChannelTypesColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ChannelTypesDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ChannelTypesDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ChannelTypesDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
