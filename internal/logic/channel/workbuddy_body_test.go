package channel

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestWorkBuddyBodyCompatibility(t *testing.T) {
	cases := []struct {
		name, body string
		changed    bool
	}{
		{"普通请求保持字节", `{"messages":[{"role":"system","content":"普通规则"},{"role":"user","content":"你好"}],"tools":[],"max_completion_tokens":65536}`, false},
		{"系统身份开头", `{"messages":[{"role":"system","content":"You are an AI agent powered by DeepSeek Harness.\n保留规则"}]}`, true},
		{"开发者角色", `{"messages":[{"role":"developer","content":"用户规则"}]}`, true},
		{"补齐系统首条", `{"messages":[{"role":"user","content":"你好"}]}`, true},
		{"历史数组每段", `{"messages":[{"role":"system","content":"规则"},{"role":"assistant","content":[{"type":"text","text":"普通内容"},{"type":"text","text":" You are an AI agent powered by DeepSeek Harness"},{"type":"image_url","image_url":{"url":"https://example.com/x"}}],"tool_calls":[{"id":"call1"}]}]}`, true},
		{"不改用户与工具", `{"messages":[{"role":"system","content":"规则"},{"role":"user","content":"You are an AI agent powered by DeepSeek Harness"},{"role":"tool","content":"You are an AI agent powered by DeepSeek Harness"}]}`, false},
		{"不匹配中间内容", `{"messages":[{"role":"system","content":"规则 You are an AI agent powered by DeepSeek Harness"}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyWorkBuddyBody([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if (!bytes.Equal(got, []byte(tc.body))) != tc.changed {
				t.Fatalf("unexpected change: %s", got)
			}
			again, err := ApplyWorkBuddyBody(got)
			if err != nil || !bytes.Equal(got, again) {
				t.Fatalf("not idempotent: %s %v", again, err)
			}
			var before, after map[string]json.RawMessage
			_ = json.Unmarshal([]byte(tc.body), &before)
			_ = json.Unmarshal(got, &after)
			for k, v := range before {
				if k != "messages" && !bytes.Equal(v, after[k]) {
					t.Fatalf("modified unrelated field %s", k)
				}
			}
		})
	}
}

func TestWorkBuddyNeutralContentPreservesRules(t *testing.T) {
	original := "  YOU ARE AN AI AGENT POWERED BY DEEPSEEK HARNESS.\n规则与工具说明保留"
	got, changed := workBuddyNeutralText(original)
	if !changed || got != workBuddyFallbackSystemPrompt+"\n\n"+original {
		t.Fatal("original text lost")
	}
}
