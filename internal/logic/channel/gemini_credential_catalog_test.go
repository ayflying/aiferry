package channel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
)

func TestGeminiCatalogSelectionUsesAccountMembership(t *testing.T) {
	// GoFrame's frame-level default DB is a singleton even after SetConfigGroup.
	// Isolate this integration database from the other channel fixtures.
	const testChild = "AIFERRY_TEST_GEMINI_CATALOG_CHILD"
	if os.Getenv(testChild) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGeminiCatalogSelectionUsesAccountMembership$")
		cmd.Env = append(os.Environ(), testChild+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("catalog integration subprocess: %v\n%s", err, output)
		}
		return
	}
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "gemini-catalog.db"))
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}); err != nil {
		t.Fatal(err)
	}
	db := g.DB()
	t.Cleanup(func() { _ = db.Close(ctx) })
	schema, err := os.ReadFile(filepath.Join("..", "app", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	exec(`INSERT INTO channels(id,type) VALUES (10,'gemini_subscription'), (20,'gemini_subscription')`)
	exec(`INSERT INTO channel_models(id,channel_id,upstream_name,public_name) VALUES (31,10,'gemini-alpha','my-alias'), (32,10,'gemini-beta','beta'), (33,10,'Gemini-alpha','wrong-case'), (34,20,'gemini-alpha','foreign')`)
	for _, fixture := range []struct{ credentialID, channelID int }{{11, 10}, {12, 10}, {13, 10}, {21, 20}} {
		exec(`INSERT INTO channel_credentials(id,channel_id,api_key_cipher) VALUES (?,?,?)`, fixture.credentialID, fixture.channelID, geminiCredentialPrefix+"fixture")
	}
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	svc := &sChannel{app: &app.Service{Redis: store}}
	sync := func(channelID, credentialID uint64, names ...string) {
		t.Helper()
		metas := make(map[string]modelmetadata.Metadata)
		for _, name := range names {
			metas[name] = modelmetadata.Metadata{}
		}
		if err := saveGeminiCredentialCatalog(ctx, channelID, credentialID, metas); err != nil {
			t.Fatal(err)
		}
	}
	sync(10, 11, "gemini-alpha")
	sync(10, 12, "gemini-beta")
	sync(20, 21, "gemini-alpha")
	if n, err := db.GetValue(ctx, `SELECT count(*) FROM model_metadata WHERE channel_id=10`); err != nil || n.Int() != 2 {
		t.Fatalf("display metadata union = %v, %v", n, err)
	}

	const apiKeyID = 9
	exec(`INSERT INTO api_key_channel_credentials(api_key_id,channel_id,channel_credential_id) VALUES (9,10,12)`)
	sticky := WithConversationAffinity(ctx, "conversation")
	svc.writeCredentialAffinity(ctx, credentialAffinityKey("conversation", 10, 31), 13)
	check := func(ctx context.Context, model uint64, excluded map[uint64]struct{}, want uint64) {
		t.Helper()
		got, err := svc.SelectCredential(ctx, apiKeyID, 10, model, excluded)
		if got.ID != want || ((err != nil) != (want == 0)) {
			t.Fatalf("model %d selected credential %d (want %d), err = %v", model, got.ID, want, err)
		}
	}
	check(ctx, 31, nil, 11)                           // API key affinity cannot use account 12 for alpha.
	check(sticky, 31, nil, 11)                        // Unsynced account 13 cannot inherit conversation affinity.
	check(sticky, 31, map[uint64]struct{}{11: {}}, 0) // Retry cannot bypass the account grant.
	check(sticky, 32, nil, 12)                        // Disjoint catalog remains available.
	check(sticky, 33, nil, 0)                         // Runtime IDs compare exactly.
	check(sticky, 34, nil, 0)                         // Another channel's model ID is rejected.
	check(sticky, 0, nil, 0)                          // No-model calls do not fail open.
	exec(`INSERT INTO channel_model_credentials(channel_id, channel_model_id, channel_credential_id, cooldown_until) VALUES (10,31,11,'2099-01-01 00:00:00')`)
	check(sticky, 31, nil, 0)
	check(WithoutCredentialComboCooldown(sticky), 31, nil, 11)
	check(WithoutCredentialComboCooldown(sticky), 31, map[uint64]struct{}{11: {}}, 0)
	if _, err := svc.geminiCredentialForModelTest(ctx, 10, 12, "gemini-alpha"); err == nil {
		t.Fatal("probe accepted an account with no matching catalog entry")
	}
	if got, err := svc.geminiCredentialForModelTest(ctx, 10, 0, "gemini-alpha"); err != nil || got.ID != 11 {
		t.Fatalf("probe chose %v, %v", got.ID, err)
	}
	sync(10, 11) // A successful empty refresh revokes only account 11's grant.
	check(WithoutCredentialComboCooldown(sticky), 31, nil, 0)
	check(sticky, 32, nil, 12)
}
