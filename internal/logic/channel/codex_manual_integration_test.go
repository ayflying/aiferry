package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcfg"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/channelgroup"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/secret"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// Isolate GoFrame's process-wide default DAO/config and never load local config.
func TestCodexManualCredentialIntegration(t *testing.T) {
	const marker = "AIFERRY_CODEX_MANUAL_TEST_CHILD"
	if os.Getenv(marker) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "-test.run=^TestCodexManualCredentialIntegration$", "-test.v")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated manual Codex test: %v\n%s", err, output)
		}
		return
	}
	adapter, err := gcfg.NewAdapterContent("{}")
	if err != nil {
		t.Fatal(err)
	}
	g.Cfg().SetAdapter(adapter)
	if err = gdb.SetConfig(gdb.Config{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "manual.db"))
	if err = gdb.SetConfigGroup("default", gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}); err != nil {
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
	if _, err = dao.Users.Ctx(ctx).Data(do.Users{Id: 1, Name: "manual-owner", Role: "user", Status: 1}).Insert(); err != nil {
		t.Fatal(err)
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
	var upstreamCalls atomic.Int32
	application := &app.Service{Redis: store, Secrets: secrets, HTTP: &http.Client{Transport: codexRoundTrip(func(*http.Request) (*http.Response, error) {
		upstreamCalls.Add(1)
		return nil, fmt.Errorf("manual tokens must never call OAuth refresh")
	})}}
	svc := &sChannel{app: application, types: channeltype.New(registry, nil), groups: channelgroup.New(store)}
	if err = store.Set(ctx, "aiferry:admin-session:manual-owner", `{"id":1}`, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	authentication := auth.New(application, nil)
	server := ghttp.GetServer(t.Name())
	server.SetPort(0)
	server.SetDumpRouterMap(false)
	server.SetAccessLogEnabled(false)
	server.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(authentication.RequireUser)
		group.POST("/{action}/{channelId}/{credentialId}", func(r *ghttp.Request) {
			var input adminapi.ChannelInput
			if err := r.Parse(&input); err != nil {
				r.Response.WriteStatus(400)
				return
			}
			var id uint64
			var callErr error
			switch r.GetRouter("action").String() {
			case "create":
				id, callErr = svc.Create(r.Context(), input)
			case "update":
				callErr = svc.Update(r.Context(), r.GetRouter("channelId").Uint64(), input)
			case "credential":
				key := ""
				if input.APIKey != nil {
					key = *input.APIKey
				}
				id, callErr = svc.CreateCredential(r.Context(), r.GetRouter("channelId").Uint64(), adminapi.ChannelCredentialInput{APIKey: key})
			case "reveal":
				var token string
				token, callErr = svc.RevealCredential(r.Context(), r.GetRouter("channelId").Uint64(), r.GetRouter("credentialId").Uint64())
				if callErr == nil {
					r.Response.WriteJson(struct {
						Token string `json:"token"`
					}{token})
					return
				}
			}
			if callErr != nil {
				r.Response.WriteStatus(400)
				return
			}
			r.Response.WriteJson(struct {
				ID uint64 `json:"id"`
			}{id})
		})
	})
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	call := func(action string, channelID, credentialID uint64, key *string) (int, uint64, string) {
		t.Helper()
		name := "manual-channel"
		if action == "update" {
			name = "invalid-update-must-not-change-name"
		}
		body, err := json.Marshal(adminapi.ChannelInput{Name: name, Type: "codex", BaseURL: "https://chatgpt.com/backend-api/codex", APIKey: key, Status: 1})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/%s/%d/%d", server.GetListenedPort(), action, channelID, credentialID), strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName(), Value: "manual-owner"})
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var output struct {
			ID    uint64 `json:"id"`
			Token string `json:"token"`
		}
		if resp.StatusCode == 200 {
			if err = json.NewDecoder(resp.Body).Decode(&output); err != nil {
				t.Fatal(err)
			}
		} else {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		return resp.StatusCode, output.ID, output.Token
	}
	jwt := func(account string, expiry int64) string {
		return codexTestJWT(fmt.Sprintf(`{"exp":%d,"https://api.openai.com/auth":{"chatgpt_account_id":%q}}`, expiry, account))
	}
	token := jwt("acct-manual", time.Now().Add(time.Hour).Unix())
	status, channelID, _ := call("create", 0, 0, &token)
	if status != 200 || channelID == 0 {
		t.Fatalf("manual Create failed: %d", status)
	}
	var channel entity.Channels
	if err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID}).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	if plain, err := secrets.Decrypt(channel.ApiKeyCipher); err != nil || plain != "" {
		t.Fatal("channel retained raw manual token")
	}
	row, credential, err := svc.latestCodexCredential(ctx, channelID, "acct-manual")
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != token || credential.RefreshToken != "" || credential.ChannelID != channelID || credential.Seal == "" || row.KeyHash != codexAccountHash("acct-manual") {
		t.Fatal("manual credential lost seal/account/hash or invented refresh token")
	}
	if strings.Contains(row.ApiKeyCipher, token) || strings.Contains(row.ApiKeyCipher, codexCredentialPrefix) {
		t.Fatal("credential not encrypted at rest")
	}
	if seal, err := secrets.Decrypt(credential.Seal); err != nil || seal != fmt.Sprintf("codex:%d:acct-manual", channelID) {
		t.Fatal("invalid channel-bound seal")
	}
	if status, _, revealed := call("reveal", channelID, row.Id, nil); status != 200 || revealed != token {
		t.Fatal("RevealCredential must return JWT, not envelope or seal")
	}
	request, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err = svc.ApplyUpstreamAuthHeaders(request, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: row.ApiKeyCipher}); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Authorization") != "Bearer "+token || request.Header.Get("ChatGPT-Account-Id") != "acct-manual" {
		t.Fatal("manual credential auth headers incorrect")
	}

	for _, key := range []*string{nil, new(string)} {
		status, id, _ := call("create", 0, 0, key)
		if status != 200 || id == 0 {
			t.Fatalf("empty Create failed: %d", status)
		}
		count, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: id}).Count()
		if err != nil || count != 0 {
			t.Fatal("empty Create created a credential")
		}
	}
	second := jwt("acct-second", time.Now().Add(time.Hour).Unix())
	if status, id, _ := call("credential", channelID, 0, &second); status != 200 || id == 0 {
		t.Fatalf("CreateCredential failed: %d", status)
	}
	rotated := jwt("acct-second", time.Now().Add(2*time.Hour).Unix())
	if status, _, _ := call("credential", channelID, 0, &rotated); status < 400 {
		t.Fatal("same-account different JWT duplicated credential")
	}
	channelsBefore, err := dao.Channels.Ctx(ctx).Count()
	if err != nil {
		t.Fatal(err)
	}
	credentialsBefore, err := dao.ChannelCredentials.Ctx(ctx).Count()
	if err != nil {
		t.Fatal(err)
	}
	var credentialsSnapshot []entity.ChannelCredentials
	if err = dao.ChannelCredentials.Ctx(ctx).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&credentialsSnapshot); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"sk-invalid", jwt("acct-expired", time.Now().Add(-time.Hour).Unix()), codexCredentialPrefix + `{"access_token":"forged"}`} {
		for _, action := range []string{"create", "update", "credential"} {
			if status, _, _ := call(action, channelID, 0, &bad); status < 400 {
				t.Fatalf("%s accepted invalid manual token", action)
			}
		}
	}
	channelsAfter, err := dao.Channels.Ctx(ctx).Count()
	if err != nil {
		t.Fatal(err)
	}
	credentialsAfter, err := dao.ChannelCredentials.Ctx(ctx).Count()
	if err != nil {
		t.Fatal(err)
	}
	var unchanged entity.Channels
	if err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID}).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(channel)
	afterJSON, _ := json.Marshal(unchanged)
	var credentialsUnchanged []entity.ChannelCredentials
	if err = dao.ChannelCredentials.Ctx(ctx).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&credentialsUnchanged); err != nil {
		t.Fatal(err)
	}
	credentialsBeforeJSON, _ := json.Marshal(credentialsSnapshot)
	credentialsAfterJSON, _ := json.Marshal(credentialsUnchanged)
	if channelsAfter != channelsBefore || credentialsAfter != credentialsBefore || string(beforeJSON) != string(afterJSON) || string(credentialsBeforeJSON) != string(credentialsAfterJSON) {
		t.Fatal("invalid Create/Update mutated persisted channel/credential state")
	}
	credential.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	expiredCipher, err := secrets.Encrypt(encodeCodexCredential(credential))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: row.Id}).Data(do.ChannelCredentials{ApiKeyCipher: expiredCipher}).Update(); err != nil {
		t.Fatal(err)
	}
	expiredRequest, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err = svc.ApplyUpstreamAuthHeaders(expiredRequest, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: expiredCipher}); err == nil {
		t.Fatal("expired manual token accepted")
	}
	if expiredRequest.Header.Get("Authorization") != "" || upstreamCalls.Load() != 0 {
		t.Fatal("expired manual token emitted authorization or triggered OAuth refresh")
	}
}
