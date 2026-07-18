// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelCredentialCostSnapshotsDao is the data access object for the table channel_credential_cost_snapshots.
type ChannelCredentialCostSnapshotsDao struct {
	table    string                                // table is the underlying table name of the DAO.
	group    string                                // group is the database configuration group name of the current DAO.
	columns  ChannelCredentialCostSnapshotsColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler                    // handlers for customized model modification.
}

// ChannelCredentialCostSnapshotsColumns defines and stores column names for the table channel_credential_cost_snapshots.
type ChannelCredentialCostSnapshotsColumns struct {
	Id                  string //
	ChannelCredentialId string //
	Mode                string //
	UsedAmount          string //
	RemainingAmount     string //
	Currency            string //
	PeriodStart         string //
	PeriodEnd           string //
	QueriedAt           string //
	CreatedAt           string //
}

// channelCredentialCostSnapshotsColumns holds the columns for the table channel_credential_cost_snapshots.
var channelCredentialCostSnapshotsColumns = ChannelCredentialCostSnapshotsColumns{
	Id:                  "id",
	ChannelCredentialId: "channel_credential_id",
	Mode:                "mode",
	UsedAmount:          "used_amount",
	RemainingAmount:     "remaining_amount",
	Currency:            "currency",
	PeriodStart:         "period_start",
	PeriodEnd:           "period_end",
	QueriedAt:           "queried_at",
	CreatedAt:           "created_at",
}

// NewChannelCredentialCostSnapshotsDao creates and returns a new DAO object for table data access.
func NewChannelCredentialCostSnapshotsDao(handlers ...gdb.ModelHandler) *ChannelCredentialCostSnapshotsDao {
	return &ChannelCredentialCostSnapshotsDao{
		group:    "default",
		table:    "channel_credential_cost_snapshots",
		columns:  channelCredentialCostSnapshotsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ChannelCredentialCostSnapshotsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ChannelCredentialCostSnapshotsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ChannelCredentialCostSnapshotsDao) Columns() ChannelCredentialCostSnapshotsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ChannelCredentialCostSnapshotsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ChannelCredentialCostSnapshotsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ChannelCredentialCostSnapshotsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
