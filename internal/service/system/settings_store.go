package system

import (
	"database/sql"
	"errors"
)

func isNoRowsError(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
