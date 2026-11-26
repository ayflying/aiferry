package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

func TestGeminiOAuthOwnerCallbackAndProxyPolicy(t *testing.T) {
	ctx := context.Background()
	t.Setenv("AIFERRY_GEMINI_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("AIFERRY_GEMINI_OAUTH_CLIENT_SECRET", "fixture-secret")
	t.Setenv("AIFERRY_GEMINI_OAUTH_REDIRECT_URI", "https://aiferry.example"+GeminiOAuthCallbackPath)
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "gemini-login.db"))
	if err := gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}); err != nil {
		t.Fatal(err)
	}
	db := g.DB("default")
	t.Cleanup(func() { _ = db.Close(ctx) })
	schema, err := os.ReadFile(filepath.Join("..", "app", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err = db.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, statement := range []string{
		"INSERT INTO users(id,name,role,status) VALUES(1,'owner','user',1),(2,'other','user',1)",
		"INSERT INTO channels(id,name,type,base_url,created_by_user_id,proxy_url_cipher) VALUES(10,'own','gemini_subscription','https://cloudcode-pa.googleapis.com',1,NULL),(11,'own-other','gemini_subscription','https://cloudcode-pa.googleapis.com',1,'http://127.0.0.1:1234'),(20,'foreign','gemini_subscription','https://cloudcode-pa.googleapis.com',2,'http://127.0.0.1:1234')",
		"UPDATE channels SET project_id='legacy-project' WHERE id=11",
	} {
		if _, err = db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := config.LoadBuiltins(filepath.Join("..", "..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	application := &app.Service{Redis: store, Secrets: secret.New(), HTTP: &http.Client{}}
	svc := &sChannel{app: application, types: channeltype.New(registry, nil)}
	authentication := auth.New(application, nil)
	for session, user := range map[string]int{"gemini-owner": 1, "gemini-other": 2} {
		if err = store.Set(ctx, "aiferry:admin-session:"+session, fmt.Sprintf(`{"id":%d}`, user), time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	server := ghttp.GetServer(t.Name())
	server.SetPort(0)
	server.SetDumpRouterMap(false)
	server.SetAccessLogEnabled(false)
	server.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(authentication.RequireUser)
		group.POST("/{channelId}/{action}", func(r *ghttp.Request) {
			id := r.GetRouter("channelId").Uint64()
			var result any
			var err error
			switch r.GetRouter("action").String() {
			case "start":
				input := adminapi.ChannelLoginStartInput{ProjectID: r.Get("projectId").String()}
				if target := r.Get("credentialId").Uint64(); target != 0 {
					input.CredentialID = &target
				}
				result, err = svc.StartChannelLogin(r.Context(), id, input)
			case "import":
				result, err = svc.CreateCredential(r.Context(), id, adminapi.ChannelCredentialInput{APIKey: "manual-fixture"})
			case "reveal":
				result, err = svc.RevealCredential(r.Context(), id, r.Get("state").Uint64())
			default:
				result, err = svc.PollChannelLogin(r.Context(), id, r.Get("state").String())
			}
			if err != nil {
				r.Response.WriteStatus(400, err.Error())
				return
			}
			r.Response.WriteJson(result)
		})
	})
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	call := func(id int, action, state, session string, out any) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/%d/%s?state=%s&projectId=%s", server.GetListenedPort(), id, action, url.QueryEscape(state), url.QueryEscape(state)), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName(), Value: session})
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == 200 && out != nil {
			if err = json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode
	}
	if call(10, "start", "account-project", "gemini-owner", nil) != 400 || call(20, "start", "account-project", "gemini-owner", nil) != 400 || call(11, "start", "", "gemini-owner", nil) != 400 || call(11, "import", "", "gemini-owner", nil) != 400 {
		t.Fatal("start/import accepted missing proxy, wrong owner, or raw credential")
	}
	var session ChannelLoginSession
	if call(11, "start", "account-project", "gemini-owner", &session) != 200 || !validGeminiLoginState(session.State) {
		t.Fatal("could not start valid owner/proxy login")
	}
	var pending ChannelLoginResult
	if call(11, "poll", session.State, "gemini-owner", &pending) != 200 || pending.Status != "pending" {
		t.Fatal("uncalled callback should remain pending")
	}
	for _, item := range []struct {
		id   int
		user string
	}{{20, "gemini-other"}, {11, "gemini-other"}, {10, "gemini-owner"}} {
		if call(item.id, "poll", session.State, item.user, nil) < 400 {
			t.Fatal("accepted unrelated login state")
		}
	}
	if err = svc.AcceptGeminiOAuthCallback(ctx, strings.Repeat("a", 64), "code-fixture", ""); err == nil {
		t.Fatal("accepted unknown state")
	}
	if err = svc.AcceptGeminiOAuthCallback(ctx, session.State, "code-fixture", "access_denied"); err == nil {
		t.Fatal("accepted conflicting callback parameters")
	}
	if err = svc.AcceptGeminiOAuthCallback(ctx, session.State, "code-fixture", ""); err != nil {
		t.Fatal(err)
	}
	if err = svc.AcceptGeminiOAuthCallback(ctx, session.State, "different-code", ""); err == nil {
		t.Fatal("accepted second callback")
	}
	sealed, err := store.Get(ctx, geminiLoginKey(session.State)+":callback").Result()
	if err != nil || strings.Contains(sealed, "code-fixture") {
		t.Fatal("callback not sealed in cache")
	}
	if _, err = db.Exec(ctx, "UPDATE channels SET proxy_url_cipher=NULL WHERE id=11"); err != nil {
		t.Fatal(err)
	}
	if call(11, "poll", session.State, "gemini-owner", nil) != 400 {
		t.Fatal("token exchange accepted removed proxy")
	}
	if n, err := db.GetValue(ctx, "SELECT count(*) FROM channel_credentials"); err != nil || n.Int() != 0 {
		t.Fatal("unauthorized callback or proxy removal persisted a credential")
	}
	cfg, err := readGeminiOAuthConfig()
	if err != nil {
		t.Fatal(err)
	}
	credential := geminiSubscriptionCredential{Subject: "fixture-subject", ProjectID: "account-project", AccessToken: "fixture-access", RefreshToken: "fixture-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	id, err := svc.saveGeminiCredential(ctx, cfg, 11, 0, credential)
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.credentialByID(ctx, 11, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.saveGeminiCredential(ctx, cfg, 11, 0, credential); err == nil {
		t.Fatal("duplicate account should not be implicitly updated")
	}
	other := credential
	other.Subject, other.ProjectID = "fixture-different-account", "second-project"
	otherID, err := svc.saveGeminiCredential(ctx, cfg, 11, 0, other)
	if err != nil || id == otherID {
		t.Fatal("different account must have a distinct credential ID", err)
	}
	for _, target := range []uint64{id, 12345} {
		if _, err := svc.saveGeminiCredential(ctx, cfg, 11, target, other); err == nil {
			t.Fatal("relogin accepted wrong or missing target")
		}
	}
	if row, err := svc.credentialByID(ctx, 11, id); err != nil || row.ApiKeyCipher != before.ApiKeyCipher {
		t.Fatal("failed add or relogin changed existing credential")
	}
	if _, err := db.Exec(ctx, "UPDATE channel_credentials SET status=0, remark='preserved' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if same, err := svc.saveGeminiCredential(ctx, cfg, 11, id, credential); err != nil || same != id {
		t.Fatal("disabled account cannot relogin in place", err)
	}
	if row, err := svc.credentialByID(ctx, 11, id); err != nil || row.Status != 0 || row.Remark != "preserved" {
		t.Fatal("relogin did not preserve status and remark")
	}
	if _, err := db.Exec(ctx, "UPDATE channel_credentials SET status=1 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	row, err := svc.credentialByID(ctx, 11, id)
	if err != nil {
		t.Fatal(err)
	}
	if call(11, "reveal", fmt.Sprint(id), "gemini-owner", nil) != 400 || strings.Contains(row.ApiKeyCipher, credential.RefreshToken) {
		t.Fatal("OAuth credential was disclosed")
	}
	if tok, project, err := svc.GeminiSubscriptionAuthorization(ctx, 11, id, row.ApiKeyCipher, "legacy-project"); err != nil || tok != credential.AccessToken || project != "account-project" {
		t.Fatal("sealed unexpired access not resolved")
	}
	if _, err := svc.GeminiSubscriptionAccessToken(ctx, 20, id, row.ApiKeyCipher); err == nil {
		t.Fatal("resolved cross-channel access")
	}
	credential.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	if _, err = svc.saveGeminiCredential(ctx, cfg, 11, id, credential); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GeminiSubscriptionAccessToken(ctx, 11, id, row.ApiKeyCipher); err == nil || !strings.Contains(err.Error(), "必须配置代理") {
		t.Fatal("refresh accepted removed proxy")
	}
	// Simulate the project-less envelope issued before per-account projects.
	legacy := other
	legacy.ProjectID = ""
	sealed, err = cfg.seal(geminiCredentialAAD(11, geminiAccountHash(legacy.Subject)), legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyCipher := geminiCredentialPrefix + sealed
	if _, err := db.Exec(ctx, "UPDATE channel_credentials SET api_key_cipher=? WHERE id=?", legacyCipher, otherID); err != nil {
		t.Fatal(err)
	}
	if _, project, err := svc.GeminiSubscriptionAuthorization(ctx, 11, otherID, legacyCipher, " legacy-project "); err != nil || project != "legacy-project" {
		t.Fatal("legacy account did not resolve its fallback project", err)
	}
	if _, err := svc.saveGeminiCredential(ctx, cfg, 11, otherID, other); err == nil {
		t.Fatal("legacy relogin replaced existing fallback with another project")
	}
	other.ProjectID = "legacy-project"
	if same, err := svc.saveGeminiCredential(ctx, cfg, 11, otherID, other); err != nil || same != otherID {
		t.Fatal("legacy account could not reauthorize in place", err)
	}
	if _, project, err := svc.GeminiSubscriptionAuthorization(ctx, 11, otherID, legacyCipher, "different-fallback"); err != nil || project != "legacy-project" {
		t.Fatal("reauthorized legacy account did not seal the original fallback project", err)
	}
}
