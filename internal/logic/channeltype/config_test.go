package channeltype

import (
	"strings"
	"testing"
)

// 部分上游的查询接口不在渠道根地址之下：根地址带 /v1 版本前缀，余额/额度接口
// 却在 /api 下（甚至另一个域名）。此时 path 填完整地址，运行期直接使用、不再
// 拼接根地址。这里固化「配置层接受且不改写完整地址」。
func TestParseConfigAcceptsAbsoluteURLPaths(t *testing.T) {
	const accountURL = "https://tokenrhythm.studio/api/user/self"
	config, err := ParseConfig([]byte(`{
    "baseUrl": "https://tokenrhythm.studio/v1",
    "models": {"path": "https://tokenrhythm.studio/api/models", "idPath": "id"},
    "costs": {
      "adapter": "custom_json", "valueType": "cost", "path": "` + accountURL + `",
      "authType": "channel_key", "remainingPath": "data.quota", "currencyPath": "data.currency"
    },
    "quota": {"adapter": "zhipu_coding_plan", "path": "` + accountURL + `", "authType": "channel_key"}
  }`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Costs.Path != accountURL {
		t.Fatalf("costs.path was rewritten: %q", config.Costs.Path)
	}
	if config.Quota.Path != accountURL {
		t.Fatalf("quota.path was rewritten: %q", config.Quota.Path)
	}
	if config.Models.Path != "https://tokenrhythm.studio/api/models" {
		t.Fatalf("models.path was rewritten: %q", config.Models.Path)
	}
}

// 额度路径的相对写法按根地址的 host 根解析，不是拼在根地址之后，所以漏掉前导
// 斜杠的写法必须报错——否则会静默查错地址。
func TestParseConfigRejectsQuotaPathWithoutLeadingSlash(t *testing.T) {
	_, err := ParseConfig([]byte(`{
    "baseUrl": "https://api.example.com/v1",
    "models": {"path": "/models", "idPath": "id"},
    "costs": {"adapter": "none"},
    "quota": {"adapter": "zhipu_coding_plan", "path": "api/monitor/usage/quota/limit"}
  }`))
	if err == nil {
		t.Fatal("expected quota.path shape rejection")
	}
	if !strings.Contains(err.Error(), "quota.path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsAbsoluteHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"https://host/api": true,
		"http://host":      true,
		"HTTPS://HOST/api": true,
		"https://":         false,
		"/api/user/self":   false,
		"api/user/self":    false,
		"ftp://host/api":   false,
		"//host/api":       false,
		"":                 false,
	}
	for value, want := range cases {
		if got := IsAbsoluteHTTPURL(value); got != want {
			t.Errorf("IsAbsoluteHTTPURL(%q) = %t, want %t", value, got, want)
		}
	}
}
