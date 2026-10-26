package channel

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPerturbWorkBuddyBody(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are an AI agent powered by DeepSeek Harness. 规则。保留"},{"role":"assistant","content":[{"type":"text","text":"Answer. Keep"}]},{"role":"user","content":"你好。请回答"}],"tools":[{"type":"function"}]}`)
	got, changed := PerturbWorkBuddyBody(body)
	if !changed {
		t.Fatal("应扰动 system/assistant 文本")
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(body, &before)
	if err := json.Unmarshal(got, &after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before["tools"], after["tools"]) {
		t.Fatal("工具定义不得改动")
	}
	var msgs []map[string]json.RawMessage
	_ = json.Unmarshal(after["messages"], &msgs)
	var user string
	_ = json.Unmarshal(msgs[2]["content"], &user)
	if user != "你好。请回答" {
		t.Fatalf("user 消息不得改动: %q", user)
	}
	var system string
	_ = json.Unmarshal(msgs[0]["content"], &system)
	if !strings.Contains(system, "DeepSeek\u200b Harness") {
		t.Fatalf("应在专有名词内部插入零宽空格: %q", system)
	}
	if strings.ReplaceAll(strings.ReplaceAll(system, "\u200b", ""), "  ", " ") == "" {
		t.Fatal("去掉零宽空格后语义必须保持")
	}
	if again, ok := PerturbWorkBuddyBody(got); ok || !bytes.Equal(again, got) {
		t.Fatal("重复扰动必须幂等")
	}
}

func TestPerturbWorkBuddyBodyNoChange(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"role":"user","content":"Hello there"}]}`,
		`{"messages":[{"role":"system","content":""}]}`,
		`not-json`,
	} {
		if got, ok := PerturbWorkBuddyBody([]byte(body)); ok || !bytes.Equal(got, []byte(body)) {
			t.Fatalf("不应改动: %s", body)
		}
	}
}

// 0.6.25 起转发链路对 workbuddy chat 请求「首跳即扰动」：先中和前缀、再插零宽空格，
// 两个变换叠加后首次上游调用就不再命中渠道指纹。这里锁定组合顺序与幂等性。
func TestWorkBuddyNeutralizeThenPerturb(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are an AI agent powered by DeepSeek Harness. 规则。保留"}]}`)
	neutralized, err := ApplyWorkBuddyBody(body)
	if err != nil {
		t.Fatal(err)
	}
	perturbed, changed := PerturbWorkBuddyBody(neutralized)
	if !changed {
		t.Fatal("中和后的体仍应可扰动")
	}
	var out struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(perturbed, &out); err != nil {
		t.Fatal(err)
	}
	system := out.Messages[0].Content
	if !strings.Contains(system, "helpful assistant") {
		t.Fatalf("应包含中性首行前缀: %q", system)
	}
	if !strings.Contains(system, "DeepSeek\u200b Harness") {
		t.Fatalf("应在专有名词内部插入零宽空格: %q", system)
	}
	if again, ok := PerturbWorkBuddyBody(perturbed); ok || !bytes.Equal(again, perturbed) {
		t.Fatal("组合变换必须幂等")
	}
}

func TestIsWorkBuddyGateFailure(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		status  int
		body    string
		expects bool
	}{
		{"11128 命中", "workbuddy", 400, `{"code":11128,"msg":"Illegal API invocation from an unapproved channel"}`, true},
		{"带空格命中", "workbuddy", 400, `{"code": 11128}`, true},
		{"非 workbuddy 渠道", "openai", 400, `{"code":11128}`, false},
		{"其它错误码", "workbuddy", 400, `{"code":11129}`, false},
		{"非 400", "workbuddy", 429, `{"code":11128}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsWorkBuddyGateFailure(tc.typ, tc.status, []byte(tc.body)); got != tc.expects {
				t.Fatalf("got %v want %v", got, tc.expects)
			}
		})
	}
}
