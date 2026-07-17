package system

import (
	"database/sql"
	"fmt"
	"testing"
)

func TestIsNoRowsError(t *testing.T) {
	if !isNoRowsError(fmt.Errorf("wrapped: %w", sql.ErrNoRows)) {
		t.Fatal("wrapped sql.ErrNoRows must be recognized")
	}
	if isNoRowsError(fmt.Errorf("unexpected failure")) {
		t.Fatal("other errors must not be recognized as missing rows")
	}
}
