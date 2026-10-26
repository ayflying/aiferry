package channel

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// quoteJSON 把字符串编码为 JSON 字符串字面量（含引号），供用例内联拼装报文。
func quoteJSON(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

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
	if strings.Contains(system, "DeepSeek Harness") {
		t.Fatalf("DeepSeek 与 Harness 之间必须插入零宽空格: %q", system)
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
	if strings.Contains(system, "DeepSeek Harness") {
		t.Fatalf("DeepSeek 与 Harness 之间必须插入零宽空格: %q", system)
	}
	if again, ok := PerturbWorkBuddyBody(perturbed); ok || !bytes.Equal(again, perturbed) {
		t.Fatal("组合变换必须幂等")
	}
}

// 0.6.26 生产实证（afreq_862d819d）：「You are a coding agent powered by the
// hy3 model.」没有相邻大写词对、没有句中逗号、句点落在指纹之外，稀疏扰动的
// 全部规则对它零命中，三发扰动体仍被 11128 拒绝。身份从句必须在从句内部
// 每个词边界打断；模型名之后的可变尾部不在匹配范围内，保持原样。
func TestWorkBuddyIdentityClauseBreak(t *testing.T) {
	const z = "\u200b"
	cases := []struct {
		name  string
		text  string
		want  string
		plain string
	}{
		{
			// 从句打断把零宽空格插在每个空白之后；“the hy3 model.” 是
			// 可变尾部，不在匹配范围内，保持原样。
			name:  "powered by 从句",
			text:  "You are a coding agent powered by the hy3 model.",
			want:  "You " + z + "are " + z + "a " + z + "coding " + z + "agent " + z + "powered " + z + "by the hy3 model.",
			plain: "You are a coding agent powered by the hy3 model.",
		},
		{
			// (?i) 大小写不敏感；从句在 “Harness” 处结束，其后的 Web GUI
			// 交给稀疏扰动的专有名词规则。
			name:  "interacting 从句大小写不敏感",
			text:  "Please. You are interacting with the user through the DeepSeek Harness Web GUI.",
			want:  "You " + z + "are " + z + "interacting " + z + "with " + z + "the " + z + "user " + z + "through " + z + "the " + z + "DeepSeek " + z + "Harness",
			plain: "Please. You are interacting with the user through the DeepSeek Harness Web GUI.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"messages":[{"role":"system","content":` + quoteJSON(tc.text) + `}]}`)
			got, changed := PerturbWorkBuddyBody(body)
			if !changed {
				t.Fatal("身份从句必须被打断")
			}
			var out struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(got, &out); err != nil {
				t.Fatal(err)
			}
			content := out.Messages[0].Content
			if !strings.Contains(content, tc.want) {
				t.Fatalf("打断位置不符:\n got  %q\n want(含) %q", content, tc.want)
			}
			if stripped := strings.ReplaceAll(content, z, ""); stripped != tc.plain {
				t.Fatalf("去掉零宽空格后必须还原原文: %q", stripped)
			}
			if again, ok := PerturbWorkBuddyBody(got); ok || !bytes.Equal(again, got) {
				t.Fatal("身份从句打断必须幂等")
			}
		})
	}
}

// 身份从句打断与稀疏扰动的叠加：先逐词打断从句，稀疏扰动再补句读与
// 专有名词边界；两步各自幂等，叠加后整体幂等。
func TestWorkBuddyIdentityClauseBreakAfterSparse(t *testing.T) {
	const z = "\u200b"
	text := "You are a coding agent powered by the hy3 model. DeepSeek Harness 规则。"
	body := []byte(`{"messages":[{"role":"system","content":` + quoteJSON(text) + `}]}`)
	got, changed := PerturbWorkBuddyBody(body)
	if !changed {
		t.Fatal("应同时执行从句打断与稀疏扰动")
	}
	var out struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	system := out.Messages[0].Content
	if strings.Contains(system, "DeepSeek Harness") {
		t.Fatalf("DeepSeek 与 Harness 之间必须有零宽空格: %q", system)
	}
	if !strings.Contains(system, "You "+z+"are "+z+"a "+z+"coding") {
		t.Fatalf("身份从句应被逐词打断: %q", system)
	}
	if stripped := strings.ReplaceAll(system, z, ""); stripped != text {
		t.Fatalf("去掉零宽空格后必须还原原文: %q", stripped)
	}
	if again, ok := PerturbWorkBuddyBody(got); ok || !bytes.Equal(again, got) {
		t.Fatal("叠加变换必须幂等")
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
