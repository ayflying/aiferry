package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
)

func TestWorkerCredentialRemarkUpgrade(t *testing.T) {
	ctx := context.Background()
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Link: sqliteLink(filepath.Join(t.TempDir(), "old-worker.db"))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(ctx) })
	if _, err = db.Exec(ctx, "CREATE TABLE channel_credentials(id INTEGER PRIMARY KEY, api_key_cipher TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "INSERT INTO channel_credentials VALUES(1,'preserved-cipher')"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = execSchema(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	row, err := db.GetOne(ctx, "SELECT api_key_cipher,remark FROM channel_credentials WHERE id=1")
	if err != nil || row["api_key_cipher"].String() != "preserved-cipher" || row["remark"].String() != "" {
		t.Fatalf("old worker upgrade lost values: %v %v", row, err)
	}
	if _, err = db.Exec(ctx, "UPDATE channel_credentials SET remark='账号说明' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err = execSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	value, err := db.GetValue(ctx, "SELECT remark FROM channel_credentials WHERE id=1")
	if err != nil || value.String() != "账号说明" {
		t.Fatalf("restart lost remark: %v %v", value, err)
	}
}
