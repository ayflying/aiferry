package channel

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

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
