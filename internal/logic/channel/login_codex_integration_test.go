package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// 使用真实 SQLite、会话缓存与认证中间件；仅官方 HTTP 传输被 fixture 替换。
func TestCodexLoginPersistenceAndIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "codex-login.db"))
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
		"INSERT INTO channels(id,name,type,base_url,created_by_user_id) VALUES(10,'own','codex','https://chatgpt.com/backend-api/codex',1),(11,'own-other','codex','https://chatgpt.com/backend-api/codex',1),(20,'foreign','codex','https://chatgpt.com/backend-api/codex',2)",
	} {
		if _, err = db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := config.LoadBuiltins(filepath.Join("..", "..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	var starts, polls, exchanges, refreshes atomic.Int32
	var raceCipher string
	client := &http.Client{Transport: codexRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "auth.openai.com" || r.Method != http.MethodPost {
			t.Errorf("unexpected auth destination: %s %s", r.Method, r.URL)
		}
		status, body := 200, ""
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			starts.Add(1)
			body = `{"device_auth_id":"fixture-device","user_code":"ABCD-EFGH","interval":"5"}`
		case "/api/accounts/deviceauth/token":
			if polls.Add(1) == 1 {
				status, body = 403, `{}`
			} else {
				body = `{"authorization_code":"fixture-code","code_verifier":"fixture-verifier"}`
			}
		case "/oauth/token":
			payload, _ := io.ReadAll(r.Body)
			if r.Header.Get("Content-Type") == "application/json" {
				refreshes.Add(1)
				if !strings.Contains(string(payload), `"refresh_token":"refresh-initial"`) {
					t.Errorf("unexpected refresh payload")
				}
				if raceCipher != "" {
					if _, err := db.Exec(ctx, "UPDATE channel_credentials SET api_key_cipher=? WHERE channel_id=10", raceCipher); err != nil {
						t.Error(err)
					}
				}
				body = `{"access_token":"access-refreshed","refresh_token":"refresh-rotated","expires_in":3600}`
			} else {
				exchanges.Add(1)
				if !strings.Contains(string(payload), "code=fixture-code") || !strings.Contains(string(payload), "code_verifier=fixture-verifier") {
					t.Errorf("unexpected exchange payload")
				}
				body = fmt.Sprintf(`{"access_token":"access-initial","refresh_token":"refresh-initial","id_token":%q,"expires_in":3600}`, codexTestJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-integration"}}`))
			}
		default:
			t.Errorf("unexpected auth path: %s", r.URL.Path)
			status, body = 404, `{}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	application := &app.Service{Redis: store, Secrets: secrets, HTTP: client}
	svc := &sChannel{app: application, types: channeltype.New(registry, nil)}
	authentication := auth.New(application, nil)
	for session, user := range map[string]int{"codex-owner": 1, "codex-other": 2} {
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
			var value any
			var callErr error
			if r.GetRouter("action").String() == "update" {
				key := r.Get("key").String()
				callErr = svc.Update(r.Context(), id, adminapi.ChannelInput{Name: "must-not-change", Type: "codex", BaseURL: "https://chatgpt.com/backend-api/codex", APIKey: &key})
				value = map[string]bool{"updated": callErr == nil}
			} else if r.GetRouter("action").String() == "start" {
				value, callErr = svc.StartChannelLogin(r.Context(), id)
			} else {
				value, callErr = svc.PollChannelLogin(r.Context(), id, r.Get("state").String())
			}
			if callErr != nil {
				t.Logf("fixture login %s channel=%d: %v", r.GetRouter("action").String(), id, callErr)
				r.Response.WriteStatus(400)
				return
			}
			r.Response.WriteJson(value)
		})
	})
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	call := func(channel int, action, state, session string, out any) int {
		t.Helper()
		query := url.Values{"state": {state}}
		if action == "update" {
			query.Set("key", state)
		}
		req, err := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/%d/%s?%s", server.GetListenedPort(), channel, action, query.Encode()), nil)
		if err != nil {
			t.Fatal(err)
		}
		if session != "" {
			req.AddCookie(&http.Cookie{Name: auth.SessionCookieName(), Value: session})
		}
		response, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode == 200 && out != nil {
			if err := json.NewDecoder(response.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
		return response.StatusCode
	}
	for _, item := range []struct {
		channel int
		user    string
	}{{20, "codex-owner"}, {10, ""}} {
		if status := call(item.channel, "start", "", item.user, nil); status < 400 {
			t.Fatal("unauthorized device login start accepted")
		}
	}
	forged := codexFixtureCredential()
	forged.Seal = ""
	forgedCipher, err := secrets.Encrypt(encodeCodexCredential(forged))
	if err != nil {
		t.Fatal(err)
	}
	forgedRequest, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err := svc.ApplyUpstreamAuthHeaders(forgedRequest, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: forgedCipher}); err == nil {
		t.Fatal("forged envelope without seal was accepted")
	}
	for _, key := range []string{"sk-not-an-oauth-login", encodeCodexCredential(forged)} {
		if got := call(10, "update", key, "codex-owner", nil); got < 400 {
			t.Fatal("Update imported an API key into Codex")
		}
		name, err := db.GetValue(ctx, "SELECT name FROM channels WHERE id=10")
		if err != nil || name.String() != "own" {
			t.Fatal("rejected Update changed channel before validating API key")
		}
		count, err := db.GetValue(ctx, "SELECT COUNT(*) FROM channel_credentials WHERE channel_id=10")
		if err != nil || count.Int() != 0 {
			t.Fatal("rejected Update persisted credential")
		}
	}
	if starts.Load() != 0 {
		t.Fatal("invalid login attempts reached official auth")
	}
	var session ChannelLoginSession
	if status := call(10, "start", "", "codex-owner", &session); status != 200 || session.State == "" || session.UserCode != "ABCD-EFGH" || session.AuthURL != codexIssuer+"/codex/device" {
		t.Fatalf("start: %d %+v", status, session)
	}
	cachedState, err := store.Get(ctx, "aiferry:codex-login:"+session.State).Result()
	if err != nil || strings.Contains(cachedState, "fixture-device") || strings.Contains(cachedState, "ABCD-EFGH") {
		t.Fatal("device authorization state was not encrypted")
	}
	for _, item := range []struct {
		channel int
		user    string
	}{{11, "codex-owner"}, {20, "codex-other"}, {10, "codex-other"}, {10, ""}} {
		if got := call(item.channel, "poll", session.State, item.user, nil); got < 400 {
			t.Fatalf("cross-boundary state accepted: %+v", item)
		}
	}
	if polls.Load() != 0 {
		t.Fatal("invalid sessions reached upstream")
	}
	var pending ChannelLoginResult
	if status := call(10, "poll", session.State, "codex-owner", &pending); status != 200 || pending.Status != "pending" {
		t.Fatalf("pending: %d %+v", status, pending)
	}
	if status := call(10, "poll", session.State, "codex-owner", &pending); status != 200 || pending.Status != "pending" || polls.Load() != 1 {
		t.Fatal("poll throttle did not avoid duplicate upstream call")
	}
	// 清掉 fixture 缓存 gate 模拟轮询周期经过，不依赖真实等待。
	if err := store.Del(ctx, "aiferry:codex-login-poll:"+session.State).Err(); err != nil {
		t.Fatal(err)
	}
	var completed ChannelLoginResult
	if status := call(10, "poll", session.State, "codex-owner", &completed); status != 200 || completed.Status != "completed" || completed.CredentialID == 0 || completed.UID != "acct-integration" {
		t.Fatalf("complete: %d %+v", status, completed)
	}
	var repeated ChannelLoginResult
	if status := call(10, "poll", session.State, "codex-owner", &repeated); status != 200 || repeated != completed || exchanges.Load() != 1 || polls.Load() != 2 {
		t.Fatalf("completion not idempotent: %d %+v", status, repeated)
	}
	for _, item := range []struct {
		channel int
		user    string
	}{{11, "codex-owner"}, {20, "codex-other"}} {
		if got := call(item.channel, "poll", session.State, item.user, nil); got < 400 {
			t.Fatal("completed state crossed ownership boundary")
		}
	}
	if got := call(10, "poll", "missing-state", "codex-owner", nil); got < 400 {
		t.Fatal("unknown state accepted")
	}
	count, err := db.GetValue(ctx, "SELECT COUNT(*) FROM channel_credentials WHERE channel_id=10")
	if err != nil || count.Int() != 1 {
		t.Fatalf("duplicate credentials: %v %v", count, err)
	}
	_, credential, err := svc.latestCodexCredential(ctx, 10, "acct-integration")
	if err != nil || credential.AccessToken != "access-initial" || credential.ChannelID != 10 {
		t.Fatalf("saved credential invalid: %v", err)
	}
	stored, err := db.GetValue(ctx, "SELECT api_key_cipher FROM channel_credentials WHERE id=?", completed.CredentialID)
	if err != nil || strings.Contains(stored.String(), "access-initial") || strings.Contains(stored.String(), "refresh-initial") {
		t.Fatal("OAuth tokens were not encrypted at rest")
	}

	// 真实持久化刷新轮转：过期 envelope 刷新后保留账号、渠道与稳定哈希。
	credential.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	expiredCipher, err := secrets.Encrypt(encodeCodexCredential(credential))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE channel_credentials SET api_key_cipher=? WHERE id=?", expiredCipher, completed.CredentialID); err != nil {
		t.Fatal(err)
	}
	next, err := svc.codexCredentialForRequest(ctx, credential)
	if err != nil || next.RefreshToken != "refresh-rotated" || next.AccessToken != "access-refreshed" || refreshes.Load() != 1 {
		t.Fatalf("refresh rotation: %v", err)
	}
	_, persisted, err := svc.latestCodexCredential(ctx, 10, credential.AccountID)
	if err != nil || persisted.RefreshToken != next.RefreshToken || persisted.ChannelID != 10 {
		t.Fatalf("refresh persistence: %v", err)
	}

	// 在换票期间模拟重新登录；旧密文 CAS 不能覆盖新登录的凭据。
	if err := store.Del(ctx, "aiferry:codex-refresh:"+codexAccountHash(credential.AccountID)+":10").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE channel_credentials SET api_key_cipher=? WHERE id=?", expiredCipher, completed.CredentialID); err != nil {
		t.Fatal(err)
	}
	newLogin := persisted
	newLogin.AccessToken = "new-login-must-survive"
	raceCipher, err = secrets.Encrypt(encodeCodexCredential(newLogin))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.codexCredentialForRequest(ctx, credential); err == nil {
		t.Fatal("CAS race must reject stale refresh")
	}
	_, survived, err := svc.latestCodexCredential(ctx, 10, credential.AccountID)
	if err != nil || survived.AccessToken != "new-login-must-survive" {
		t.Fatalf("CAS overwrote relogin: %v", err)
	}
}
