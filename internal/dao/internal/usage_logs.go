// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// UsageLogsDao is the data access object for the table usage_logs.
type UsageLogsDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  UsageLogsColumns   // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// UsageLogsColumns defines and stores column names for the table usage_logs.
type UsageLogsColumns struct {
	Id                string //
	RequestId         string //
	UserId            string //
	ApiKeyId          string //
	ChannelId         string //
	Endpoint          string //
	RequestedModel    string //
	UpstreamModel     string //
	HttpStatus        string //
	IsStream          string //
	InputTokens       string //
	CachedInputTokens string //
	OutputTokens      string //
	TotalTokens       string //
	EstimatedCost     string //
	DurationMs        string //
	FirstTokenMs      string //
	Attempts          string //
	ErrorMessage      string //
	CreatedAt         string //
}

// usageLogsColumns holds the columns for the table usage_logs.
var usageLogsColumns = UsageLogsColumns{
	Id:                "id",
	RequestId:         "request_id",
	UserId:            "user_id",
	ApiKeyId:          "api_key_id",
	ChannelId:         "channel_id",
	Endpoint:          "endpoint",
	RequestedModel:    "requested_model",
	UpstreamModel:     "upstream_model",
	HttpStatus:        "http_status",
	IsStream:          "is_stream",
	InputTokens:       "input_tokens",
	CachedInputTokens: "cached_input_tokens",
	OutputTokens:      "output_tokens",
	TotalTokens:       "total_tokens",
	EstimatedCost:     "estimated_cost",
	DurationMs:        "duration_ms",
	FirstTokenMs:      "first_token_ms",
	Attempts:          "attempts",
	ErrorMessage:      "error_message",
	CreatedAt:         "created_at",
}

// NewUsageLogsDao creates and returns a new DAO object for table data access.
func NewUsageLogsDao(handlers ...gdb.ModelHandler) *UsageLogsDao {
	return &UsageLogsDao{
		group:    "default",
		table:    "usage_logs",
		columns:  usageLogsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *UsageLogsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *UsageLogsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *UsageLogsDao) Columns() UsageLogsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *UsageLogsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *UsageLogsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *UsageLogsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
