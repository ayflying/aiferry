// ==========================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// ==========================================================================

package internal

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// PriceSourcesDao is the data access object for the table price_sources.
type PriceSourcesDao struct {
	table    string              // table is the underlying table name of the DAO.
	group    string              // group is the database configuration group name of the current DAO.
	columns  PriceSourcesColumns // columns contains all the column names of Table for convenient usage.
	handlers []gdb.ModelHandler  // handlers for customized model modification.
}

// PriceSourcesColumns defines and stores column names for the table price_sources.
type PriceSourcesColumns struct {
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

// priceSourcesColumns holds the columns for the table price_sources.
var priceSourcesColumns = PriceSourcesColumns{
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

// NewPriceSourcesDao creates and returns a new DAO object for table data access.
func NewPriceSourcesDao(handlers ...gdb.ModelHandler) *PriceSourcesDao {
	return &PriceSourcesDao{
		group:    "default",
		table:    "price_sources",
		columns:  priceSourcesColumns,
		handlers: handlers,
	}
}

// DB retrieves and returns the underlying raw database management object of the current DAO.
func (dao *PriceSourcesDao) DB() gdb.DB {
	return g.DB(dao.group)
}

// Table returns the table name of the current DAO.
func (dao *PriceSourcesDao) Table() string {
	return dao.table
}

// Columns returns all column names of the current DAO.
func (dao *PriceSourcesDao) Columns() PriceSourcesColumns {
	return dao.columns
}

// Group returns the database configuration group name of the current DAO.
func (dao *PriceSourcesDao) Group() string {
	return dao.group
}

// Ctx creates and returns a Model for the current DAO. It automatically sets the context for the current operation.
func (dao *PriceSourcesDao) Ctx(ctx context.Context) *gdb.Model {
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
func (dao *PriceSourcesDao) Transaction(ctx context.Context, f func(ctx context.Context, tx gdb.TX) error) (err error) {
	return dao.Ctx(ctx).Transaction(ctx, f)
}
