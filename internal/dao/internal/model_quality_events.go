// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ModelQualityEventsDao is the data access object for the table model_quality_events.
type ModelQualityEventsDao struct {
	table    string                    // table is the underlying table name of the DAO.
	group    string                    // group is the database configuration group name of the current DAO.
	columns  ModelQualityEventsColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler        // handlers for customized model modification.
}

// ModelQualityEventsColumns defines and stores column names for the table model_quality_events.
type ModelQualityEventsColumns struct {
	Id             string //
	RequestId      string //
	ChannelId      string //
	CredentialId   string //
	RequestedModel string //
	ExpectedModel  string //
	ObservedModel  string //
	ReasonsJson    string //
	QuestionChars  string //
	AnswerChars    string //
	CreatedAt      string //
}

// modelQualityEventsColumns holds the columns for the table model_quality_events.
var modelQualityEventsColumns = ModelQualityEventsColumns{
	Id:             "id",
	RequestId:      "request_id",
	ChannelId:      "channel_id",
	CredentialId:   "credential_id",
	RequestedModel: "requested_model",
	ExpectedModel:  "expected_model",
	ObservedModel:  "observed_model",
	ReasonsJson:    "reasons_json",
	QuestionChars:  "question_chars",
	AnswerChars:    "answer_chars",
	CreatedAt:      "created_at",
}

// NewModelQualityEventsDao creates and returns a new DAO object for table data access.
func NewModelQualityEventsDao(handlers ...gdb.ModelHandler) *ModelQualityEventsDao {
	return &ModelQualityEventsDao{
		group:    "default",
		table:    "model_quality_events",
		columns:  modelQualityEventsColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ModelQualityEventsDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ModelQualityEventsDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ModelQualityEventsDao) Columns() ModelQualityEventsColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ModelQualityEventsDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ModelQualityEventsDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ModelQualityEventsDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
