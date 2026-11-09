package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

func TestNormalizeCredentialRemark(t *testing.T) {
	for _, item := range []struct {
		input, want string
		invalid     bool
	}{
		{"", "", false}, {"  备用账号\n月底到期  ", "备用账号\n月底到期", false},
		{strings.Repeat("中", 500), strings.Repeat("中", 500), false},
		{strings.Repeat("中", 501), "", true},
	} {
		got, err := normalizeCredentialRemark(item.input)
		if (err != nil) != item.invalid || got != item.want {
			t.Fatalf("normalize remark: length=%d got=%q err=%v", len([]rune(item.input)), got, err)
		}
	}
	view := credentialView(credentialRow{Id: 1, Remark: "测试账号", ApiKeyCipher: "secret-cipher"}, channeltype.CostConfig{})
	encoded, err := json.Marshal(view)
	if err != nil || !strings.Contains(string(encoded), `"remark":"测试账号"`) || strings.Contains(string(encoded), "secret-cipher") {
		t.Fatalf("remark must be visible without exposing cipher: %s err=%v", encoded, err)
	}
}

// TestCredentialRemarkPersistence 用真实SQLite与认证中间件验证服务层：
// 保存/清空可持久化，且不能跨用户或跨渠道修改密钥，原密文与启停状态保持不变。
func TestCredentialRemarkPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "credential-remark.db"))
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
		"INSERT INTO channels(id,name,created_by_user_id) VALUES(10,'own',1),(20,'other',2)",
		"INSERT INTO channel_credentials(id,channel_id,key_prefix,key_hash,api_key_cipher,status,remark) VALUES(11,10,'masked','hash','unchanged-cipher',0,'old'),(21,20,'masked2','hash2','other-cipher',1,'private')",
	} {
		if _, err = db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	store, err := cache.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	application := &app.Service{Redis: store}
	authentication := auth.New(application, nil)
	if err = store.Set(ctx, "aiferry:admin-session:remark-test", `{"id":1}`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	svc := &sChannel{}
	// 使用空配置创建独立HTTP服务，避免加载应用的静态目录与生产监听配置。
	server := ghttp.GetServer(t.Name())
	server.SetPort(0)
	server.SetDumpRouterMap(false)
	server.SetAccessLogEnabled(false)
	server.Group("/", func(group *ghttp.RouterGroup) {
		group.Middleware(authentication.RequireUser)
		group.PUT("/{channelId}/{credentialId}", func(r *ghttp.Request) {
			var input adminapi.ChannelCredentialRemarkInput
			if err := r.Parse(&input); err != nil {
				r.Response.WriteStatus(400)
				return
			}
			if err := svc.SetCredentialRemark(r.Context(), r.GetRouter("channelId").Uint64(), r.GetRouter("credentialId").Uint64(), input); err != nil {
				r.Response.WriteStatus(400)
				return
			}
			r.Response.WriteStatus(204)
		})
	})
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	call := func(channelID, credentialID int, body string, authenticated bool) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://127.0.0.1:%d/%d/%d", server.GetListenedPort(), channelID, credentialID), strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.AddCookie(&http.Cookie{Name: auth.SessionCookieName(), Value: "remark-test"})
		}
		response, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	readRemark := func(id int) string {
		value, err := db.GetValue(ctx, "SELECT remark FROM channel_credentials WHERE id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		return value.String()
	}
	if got := call(10, 11, `{"remark":"  新备注  "}`, true); got != 204 || readRemark(11) != "新备注" {
		t.Fatalf("save status=%d remark=%q", got, readRemark(11))
	}
	for _, item := range []struct {
		channelID, credentialID int
		body                    string
		authenticated           bool
	}{
		{10, 11, `{}`, true}, {10, 11, `{"remark":null}`, true},
		{10, 11, `{"remark":"` + strings.Repeat("长", 501) + `"}`, true},
		{20, 21, `{"remark":"越权"}`, true}, {10, 21, `{"remark":"跨渠道"}`, true},
		{10, 999, `{"remark":"不存在"}`, true}, {10, 11, `{"remark":"未登录"}`, false},
	} {
		if got := call(item.channelID, item.credentialID, item.body, item.authenticated); got < 400 {
			t.Fatalf("invalid update unexpectedly succeeded: %+v status=%d", item, got)
		}
	}
	if readRemark(11) != "新备注" || readRemark(21) != "private" {
		t.Fatal("invalid writes changed remarks")
	}
	if got := call(10, 11, `{"remark":""}`, true); got != 204 || readRemark(11) != "" {
		t.Fatalf("clear status=%d remark=%q", got, readRemark(11))
	}
	row, err := db.GetOne(ctx, "SELECT api_key_cipher,status FROM channel_credentials WHERE id=11")
	if err != nil || row["api_key_cipher"].String() != "unchanged-cipher" || row["status"].Int() != 0 {
		t.Fatalf("remark update changed credential fields: %v", err)
	}
}
