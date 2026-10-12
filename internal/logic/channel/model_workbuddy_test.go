package channel

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

// 使用实际内置配置，防止解析器测试通过但渠道仍指向外层 data 对象。
func TestWorkBuddyModelDiscoveryFromBuiltinConfig(t *testing.T) {
	registry, err := config.LoadBuiltins(filepath.Join("..", "..", "..", "manifest", "builtins.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, cfg, err := channeltype.New(registry, nil).GetByCode(context.Background(), "workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.ListPath != "data.models" || cfg.Models.IDPath != "id" {
		t.Fatalf("unexpected WorkBuddy model paths: %+v", cfg.Models)
	}
	if !cfg.Protocol.ChatCompletionsOnly || !cfg.Protocol.ForceUpstreamStream {
		t.Fatal("WorkBuddy must declare streaming chat completions")
	}
	settings := adminapi.SystemResilienceSettingsInput{}
	for _, model := range []string{"hy3", "deepseek-v4-flash", "space-bunny", "step-5-preview", "gpt-5-test"} {
		endpoints := testEndpoints("auto", model, cfg, settings)
		if !reflect.DeepEqual(endpoints, []string{"chat"}) {
			t.Fatalf("%s endpoints = %v, want chat only", model, endpoints)
		}
		for _, stream := range []bool{false, true} {
			path, payload, streamed := testPayload("chat", model, stream, cfg, settings)
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var request struct {
				Model         string `json:"model"`
				Stream        bool   `json:"stream"`
				StreamOptions struct {
					IncludeUsage bool `json:"include_usage"`
				} `json:"stream_options"`
			}
			if err := json.Unmarshal(body, &request); err != nil {
				t.Fatal(err)
			}
			if path != "/chat/completions" || !streamed || !request.Stream || !request.StreamOptions.IncludeUsage || request.Model != model {
				t.Fatalf("invalid WorkBuddy chat request: path=%s streamed=%t body=%s", path, streamed, body)
			}
		}
	}
	// 未声明限制的渠道仍遵守界面选择，不误开启流式。
	_, _, streamed := testPayload("chat", "hy3", false, channeltype.Config{}, settings)
	if streamed {
		t.Fatal("undeclared channel unexpectedly forced stream")
	}
	cases := []struct {
		name string
		body string
		want []string
		fail bool
	}{
		{"models in data object", `{"code":0,"data":{"models":[{"id":"gpt-6-sol"},{"id":"deepseek-flash"},{"id":"gpt-6-sol"},{"id":""}]}}`, []string{"deepseek-flash", "gpt-6-sol"}, false},
		{"empty models", `{"code":0,"data":{"models":[]}}`, []string{}, false},
		{"missing models", `{"code":0,"data":{}}`, nil, true},
		{"invalid models", `{"code":0,"data":{"models":{}}}`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			names, namesErr := modelNamesFromJSON(body, cfg.Models.ListPath, cfg.Models.IDPath)
			metadata, metadataErr := modelMetadataFromJSON(body, cfg.Models.ListPath, cfg.Models.IDPath)
			if tc.fail {
				if namesErr == nil || metadataErr == nil {
					t.Fatalf("invalid response accepted: namesErr=%v metadataErr=%v", namesErr, metadataErr)
				}
				return
			}
			if namesErr != nil || metadataErr != nil {
				t.Fatalf("parse models: namesErr=%v metadataErr=%v", namesErr, metadataErr)
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("names = %v, want %v", names, tc.want)
			}
			if len(metadata) != len(tc.want) {
				t.Fatalf("metadata count = %d, want %d", len(metadata), len(tc.want))
			}
			for _, name := range tc.want {
				if _, ok := metadata[name]; !ok {
					t.Fatalf("missing metadata for %s", name)
				}
			}
		})
	}
}
