// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ModelPriceRulesDao is the data access object for the table model_price_rules.
type ModelPriceRulesDao struct {
	table    string                 // table is the underlying table name of the DAO.
	group    string                 // group is the database configuration group name of the current DAO.
	columns  ModelPriceRulesColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler     // handlers for customized model modification.
}

// ModelPriceRulesColumns defines and stores column names for the table model_price_rules.
type ModelPriceRulesColumns struct {
	Id             string //
	ChannelModelId string //
	Name           string //
	Source         string //
	SourceRef      string //
	Priority       string //
	Currency       string //
	ConditionsJson string //
	RatesJson      string //
	Status         string //
	SyncedAt       string //
	CreatedAt      string //
	UpdatedAt      string //
	DeletedAt      string //
}

// modelPriceRulesColumns holds the columns for the table model_price_rules.
var modelPriceRulesColumns = ModelPriceRulesColumns{
	Id:             "id",
	ChannelModelId: "channel_model_id",
	Name:           "name",
	Source:         "source",
	SourceRef:      "source_ref",
	Priority:       "priority",
	Currency:       "currency",
	ConditionsJson: "conditions_json",
	RatesJson:      "rates_json",
	Status:         "status",
	SyncedAt:       "synced_at",
	CreatedAt:      "created_at",
	UpdatedAt:      "updated_at",
	DeletedAt:      "deleted_at",
}

// NewModelPriceRulesDao creates and returns a new DAO object for table data access.
func NewModelPriceRulesDao(handlers ...gdb.ModelHandler) *ModelPriceRulesDao {
	return &ModelPriceRulesDao{
		group:    "default",
		table:    "model_price_rules",
		columns:  modelPriceRulesColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ModelPriceRulesDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ModelPriceRulesDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ModelPriceRulesDao) Columns() ModelPriceRulesColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ModelPriceRulesDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ModelPriceRulesDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ModelPriceRulesDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
