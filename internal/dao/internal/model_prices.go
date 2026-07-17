// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// ModelPricesDao is the data access object for the table model_prices.
type ModelPricesDao struct {
	table    string             // table is the underlying table name of the DAO.
	group    string             // group is the database configuration group name of the current DAO.
	columns  ModelPricesColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler // handlers for customized model modification.
}

// ModelPricesColumns defines and stores column names for the table model_prices.
type ModelPricesColumns struct {
	PublicName       string //
	BillingMode      string //
	InputPrice       string //
	CachedInputPrice string //
	CacheWritePrice  string //
	OutputPrice      string //
	ImageInputPrice  string //
	AudioInputPrice  string //
	AudioOutputPrice string //
	RequestPrice     string //
	CreatedAt        string //
	UpdatedAt        string //
	DeletedAt        string //
}

// modelPricesColumns holds the columns for the table model_prices.
var modelPricesColumns = ModelPricesColumns{
	PublicName:       "public_name",
	BillingMode:      "billing_mode",
	InputPrice:       "input_price",
	CachedInputPrice: "cached_input_price",
	CacheWritePrice:  "cache_write_price",
	OutputPrice:      "output_price",
	ImageInputPrice:  "image_input_price",
	AudioInputPrice:  "audio_input_price",
	AudioOutputPrice: "audio_output_price",
	RequestPrice:     "request_price",
	CreatedAt:        "created_at",
	UpdatedAt:        "updated_at",
	DeletedAt:        "deleted_at",
}

// NewModelPricesDao creates and returns a new DAO object for table data access.
func NewModelPricesDao(handlers ...gdb.ModelHandler) *ModelPricesDao {
	return &ModelPricesDao{
		group:    "default",
		table:    "model_prices",
		columns:  modelPricesColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *ModelPricesDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *ModelPricesDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *ModelPricesDao) Columns() ModelPricesColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *ModelPricesDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *ModelPricesDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *ModelPricesDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
