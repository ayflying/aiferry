// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ChannelModelsDao is the data access object for the table channel_models.
type ChannelModelsDao struct {
	table    string               // table is the underlying table name of the DAO.
	group    string               // group is the database configuration group name of the current DAO.
	columns  ChannelModelsColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler   // handlers for customized model modification.
}

// ChannelModelsColumns defines and stores column names for the table channel_models.
type ChannelModelsColumns struct {
	Id                string //
	ChannelId         string //
	PublicName        string //
	UpstreamName      string //
	Discovered        string //
	Enabled           string //
	InputPrice        string //
	CachedInputPrice  string //
	OutputPrice       string //
	LastTestEndpoint  string //
	LastTestStatus    string //
	LastTestLatencyMs string //
	LastTestError     string //
	LastTestAt        string //
	CreatedAt         string //
	UpdatedAt         string //
	DeletedAt         string //
}

// channelModelsColumns holds the columns for the table channel_models.
var channelModelsColumns = ChannelModelsColumns{
	Id:                "id",
	ChannelId:         "channel_id",
	PublicName:        "public_name",
	UpstreamName:      "upstream_name",
	Discovered:        "discovered",
	Enabled:           "enabled",
	InputPrice:        "input_price",
	CachedInputPrice:  "cached_input_price",
	OutputPrice:       "output_price",
	LastTestEndpoint:  "last_test_endpoint",
	LastTestStatus:    "last_test_status",
	LastTestLatencyMs: "last_test_latency_ms",
	LastTestError:     "last_test_error",
	LastTestAt:        "last_test_at",
	CreatedAt:         "created_at",
	UpdatedAt:         "updated_at",
	DeletedAt:         "deleted_at",
}

// NewChannelModelsDao creates and returns a new DAO object for table data access.
func NewChannelModelsDao(handlers ...gdb.ModelHandler) *ChannelModelsDao {
	return &ChannelModelsDao{
		group:    "default",
		table:    "channel_models",
		columns:  channelModelsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ChannelModelsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ChannelModelsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ChannelModelsDao) Columns() ChannelModelsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ChannelModelsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ChannelModelsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ChannelModelsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
