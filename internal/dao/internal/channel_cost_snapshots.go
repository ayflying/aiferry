// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelCostSnapshotsDao is the data access object for the table channel_cost_snapshots.
type ChannelCostSnapshotsDao struct {
	table    string                      // table is the underlying table name of the DAO.
	group    string                      // group is the database configuration group name of the current DAO.
	columns  ChannelCostSnapshotsColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler          // handlers for customized model modification.
}

// ChannelCostSnapshotsColumns defines and stores column names for the table channel_cost_snapshots.
type ChannelCostSnapshotsColumns struct {
	Id              string //
	ChannelId       string //
	Mode            string //
	UsedAmount      string //
	RemainingAmount string //
	Currency        string //
	PeriodStart     string //
	PeriodEnd       string //
	QueriedAt       string //
	CreatedAt       string //
}

// channelCostSnapshotsColumns holds the columns for the table channel_cost_snapshots.
var channelCostSnapshotsColumns = ChannelCostSnapshotsColumns{
	Id:              "id",
	ChannelId:       "channel_id",
	Mode:            "mode",
	UsedAmount:      "used_amount",
	RemainingAmount: "remaining_amount",
	Currency:        "currency",
	PeriodStart:     "period_start",
	PeriodEnd:       "period_end",
	QueriedAt:       "queried_at",
	CreatedAt:       "created_at",
}

// NewChannelCostSnapshotsDao creates and returns a new DAO object for table data access.
func NewChannelCostSnapshotsDao(handlers ...gdb.ModelHandler) *ChannelCostSnapshotsDao {
	return &ChannelCostSnapshotsDao{
		group:    "default",
		table:    "channel_cost_snapshots",
		columns:  channelCostSnapshotsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ChannelCostSnapshotsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ChannelCostSnapshotsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ChannelCostSnapshotsDao) Columns() ChannelCostSnapshotsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ChannelCostSnapshotsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ChannelCostSnapshotsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ChannelCostSnapshotsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
