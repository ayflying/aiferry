// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelGroupsDao is the data access object for the table channel_groups.
type ChannelGroupsDao struct {
	table    string               // table is the underlying table name of the DAO.
	group    string               // group is the database configuration group name of the current DAO.
	columns  ChannelGroupsColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler   // handlers for customized model modification.
}

// ChannelGroupsColumns defines and stores column names for the table channel_groups.
type ChannelGroupsColumns struct {
	Id          string //
	Name        string //
	Code        string //
	Description string //
	Status      string //
	CreatedAt   string //
	UpdatedAt   string //
	DeletedAt   string //
}

// channelGroupsColumns holds the columns for the table channel_groups.
var channelGroupsColumns = ChannelGroupsColumns{
	Id:          "id",
	Name:        "name",
	Code:        "code",
	Description: "description",
	Status:      "status",
	CreatedAt:   "created_at",
	UpdatedAt:   "updated_at",
	DeletedAt:   "deleted_at",
}

// NewChannelGroupsDao creates and returns a new DAO object for table data access.
func NewChannelGroupsDao(handlers ...gdb.ModelHandler) *ChannelGroupsDao {
	return &ChannelGroupsDao{
		group:    "default",
		table:    "channel_groups",
		columns:  channelGroupsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ChannelGroupsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ChannelGroupsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ChannelGroupsDao) Columns() ChannelGroupsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ChannelGroupsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ChannelGroupsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ChannelGroupsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
