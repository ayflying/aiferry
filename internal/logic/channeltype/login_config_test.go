package channeltype

import "testing"

// TestParseConfigLoginDefaults 覆盖外链登录声明的规范化：platform 小写化、
// prefixPath 缺省为 /plugin。
func TestParseConfigLoginDefaults(t *testing.T) {
	config, err := ParseConfig([]byte(`{"login":{"adapter":"external_link","platform":"WorkBuddy"}}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if config.Login.Adapter != AdapterLoginExternalLink || config.Login.Platform != "workbuddy" || config.Login.PrefixPath != defaultLoginPrefixPath {
		t.Fatalf("login = %+v, want adapter=%s platform=workbuddy prefix=%s", config.Login, AdapterLoginExternalLink, defaultLoginPrefixPath)
	}
}

// TestParseConfigLoginRejects 覆盖登录声明的失败分支：外链登录必须声明 platform，
// 未知适配器与不以 / 开头的 prefixPath 都要拦下——渠道类型的配置错误会让进程
// 启动即退出，必须在这里挡住。
func TestParseConfigLoginRejects(t *testing.T) {
	cases := map[string]string{
		"missing platform": `{"login":{"adapter":"external_link"}}`,
		"unknown adapter":  `{"login":{"adapter":"wechat_qr","platform":"workbuddy"}}`,
		"bad prefix path":  `{"login":{"adapter":"external_link","platform":"workbuddy","prefixPath":"plugin"}}`,
	}
	for name, raw := range cases {
		if _, err := ParseConfig([]byte(raw)); err == nil {
			t.Fatalf("%s: ParseConfig must fail", name)
		}
	}
}

// TestParseConfigLoginNoneKeepsZero 确认未启用登录时不留残留字段。
func TestParseConfigLoginNoneKeepsZero(t *testing.T) {
	config, err := ParseConfig([]byte(`{"login":{"adapter":"none"}}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if config.Login != (LoginConfig{}) {
		t.Fatalf("login = %+v, want zero value", config.Login)
	}
}