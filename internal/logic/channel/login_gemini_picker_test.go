package channel

import (
	"context"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
)

func testGeminiProjectPicker(t *testing.T, ctx context.Context, db gdb.DB, svc *sChannel, cfg geminiOAuthConfig, call func(int, string, string, string, any, ...string) int, proxyURL string, exchanges, searches *atomic.Int32) {
	t.Helper()
	if _, err := db.Exec(ctx, "UPDATE channels SET proxy_url_cipher=? WHERE id=11", proxyURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, "UPDATE channels SET proxy_url_cipher=NULL WHERE id=11") })
	const owner = "gemini-owner"
	start := func(project string, picker bool) ChannelLoginSession {
		t.Helper()
		var session ChannelLoginSession
		if status := call(11, "start", project, owner, &session); status != 200 {
			t.Fatalf("start: HTTP %d", status)
		}
		u, err := url.Parse(session.AuthURL)
		if err != nil || strings.Contains(u.Query().Get("scope"), geminiProjectReadOnlyScope) != picker {
			t.Fatal("OAuth project scope was not limited to picker")
		}
		return session
	}
	denied := start("", true)
	if err := svc.AcceptGeminiOAuthCallback(ctx, denied.State, "", "access_denied"); err != nil {
		t.Fatal(err)
	}
	if status := call(11, "poll", denied.State, owner, nil); status != 400 {
		t.Fatalf("expected original authorization error, got HTTP %d", status)
	}
	manual := start("legacy-manual", false)
	if manual.State == denied.State || call(11, "poll", manual.State, owner, nil, "override") != 400 {
		t.Fatal("manual retry did not use a separate state or accepted project override")
	}

	picker := start("", true)
	if err := svc.AcceptGeminiOAuthCallback(ctx, picker.State, "fixture-code", ""); err != nil {
		t.Fatal(err)
	}
	session, err := svc.readGeminiLogin(ctx, cfg, picker.State)
	if err != nil || !session.ProjectPicker || session.ProjectID != "" {
		t.Fatal("new picker was not saved as unselected", err)
	}
	var result ChannelLoginResult
	// An early projectId may not skip the project_required state, even with a valid callback.
	if status := call(11, "poll", picker.State, owner, &result, "early-project"); status != 200 || result.Status != "project_required" || result.CredentialID != 0 || len(result.Projects) != 1 || result.Projects[0].ProjectID != "listed-project" || result.ProjectMessage != geminiProjectPartialMessage || exchanges.Load() != 1 || searches.Load() != 2 {
		t.Fatalf("unexpected post-exchange result: HTTP %d %+v, exchanges=%d, searches=%d", status, result, exchanges.Load(), searches.Load())
	}
	result = ChannelLoginResult{}
	if status := call(11, "poll", picker.State, owner, &result); status != 200 || result.Status != "project_required" || exchanges.Load() != 1 || searches.Load() != 2 {
		t.Fatal("poll did not reuse the post-exchange checkpoint")
	}

	if err = svc.AcceptGeminiOAuthCallback(ctx, picker.State, "another-code", ""); err == nil {
		t.Fatal("accepted a second callback during project selection")
	}
	if call(11, "poll", picker.State, "gemini-other", nil, "other-project") != 400 || call(10, "poll", picker.State, owner, nil, "other-project") != 400 || call(11, "poll", picker.State, owner, nil, "invalid/id") != 400 {
		t.Fatal("accepted foreign, wrong-channel, or invalid project confirmation")
	}
	if n, err := db.GetValue(ctx, "SELECT count(*) FROM channel_credentials"); err != nil || n.Int() != 0 {
		t.Fatal("saved credential before a valid confirmation", err)
	}
	leaseKey := geminiLoginKey(picker.State) + ":poll"
	if err = svc.app.Redis.Set(ctx, leaseKey, "other-request", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	result = ChannelLoginResult{}
	if status := call(11, "poll", picker.State, owner, &result, "manual-project"); status != 200 || result.Status != "pending" {
		t.Fatalf("confirmation bypassed concurrent lease: HTTP %d %+v", status, result)
	}
	if _, err = svc.app.Redis.Del(ctx, leaseKey).Result(); err != nil {
		t.Fatal(err)
	}
	result = ChannelLoginResult{}
	if status := call(11, "poll", picker.State, owner, &result, "manual-project"); status != 200 || result.Status != "completed" || result.CredentialID == 0 || len(result.Projects) != 0 {
		t.Fatalf("could not confirm a project not in the partial list: HTTP %d %+v", status, result)
	}
	row, committed, err := svc.readGeminiCredential(ctx, cfg, 11, result.CredentialID)
	if err != nil || committed.ProjectID != "manual-project" || strings.Contains(row.ApiKeyCipher, committed.RefreshToken) {
		t.Fatalf("stored credential was not sealed with the confirmed project: %v", err)
	}
	var repeated ChannelLoginResult
	if call(11, "poll", picker.State, owner, &repeated, "manual-project") != 200 || repeated.CredentialID != result.CredentialID || call(11, "poll", picker.State, owner, nil, "listed-project") != 400 {
		t.Fatal("completed picker was not idempotent or allowed project replacement")
	}
	current, err := svc.readGeminiLogin(ctx, cfg, picker.State)
	if err != nil || !current.ExpiresAt.Equal(session.ExpiresAt) || current.Credential != nil {
		t.Fatal("confirmation extended the session or retained its credential", err)
	}
	current.ExpiresAt = time.Now().Add(-time.Second)
	sealed, err := cfg.seal("login:"+picker.State, current)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.app.Redis.Set(ctx, geminiLoginKey(picker.State), sealed, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if call(11, "poll", picker.State, owner, nil, "manual-project") != 400 {
		t.Fatal("expired session could still be used")
	}

	partialConsent := start("", true)
	if err := svc.AcceptGeminiOAuthCallback(ctx, partialConsent.State, "fixture-code", ""); err != nil {
		t.Fatal(err)
	}
	result = ChannelLoginResult{}
	if status := call(11, "poll", partialConsent.State, owner, &result); status != 200 || result.Status != "project_required" || result.ProjectMessage != geminiProjectNoScopeMessage || len(result.Projects) != 0 || exchanges.Load() != 2 || searches.Load() != 2 {
		t.Fatalf("denial of only the optional scope did not preserve manual fallback: HTTP %d %+v", status, result)
	}
}
