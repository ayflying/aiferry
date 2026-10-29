package channel

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// realDSHSystemHead 取自 2026-10-29 生产失败报文 afreq_aaf2a19a… 的 system 开头：
// 这一份正是被上游渠道门拦下的固定指纹。
const realDSHSystemHead = "You are an AI agent powered by DeepSeek Harness.\n\nYou are a coding agent powered by the hy3 model.\n\nAgent Teams is available in this session.\n\nTokens prefixed with @ are paths the user explicitly referenced.\nRelative paths resolve from the workspace root; absolute paths identify files or directories on the host.\nA trailing slash marks a directory: list it when its contents matter.\nAnything else is a file: use the read tool when its contents are needed, and do not claim to have inspected it before reading.\n@\"...\" quotes a path containing spaces."

func TestRewriteWorkBuddyFingerprintPhrasesRealSystemHead(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"model": "hy3",
		"messages": []map[string]any{
			{"role": "system", "content": realDSHSystemHead},
			{"role": "user", "content": "继续"},
		},
	})
	got, changed := RewriteWorkBuddyFingerprintPhrases(body)
	if !changed {
		t.Fatal("真实 system 开头必须命中改词表")
	}
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	rewritten := payload.Messages[0].Content
	// 每一条被上游登记的固定指纹都必须消失；样本里出现过的还必须换成同义写法。
	for _, rule := range workBuddyFingerprintRewrites {
		if strings.Contains(rewritten, rule.from) {
			t.Fatalf("改写后仍含原指纹: %q", rule.from)
		}
		if strings.Contains(realDSHSystemHead, rule.from) && !strings.Contains(rewritten, rule.to) {
			t.Fatalf("改写后缺少同义写法: %q", rule.to)
		}
	}
	// 用户消息逐字节不变：上游不扫 user，改用户原话会凭空改变对话。
	if payload.Messages[1].Content != "继续" {
		t.Fatalf("user 消息不得改动: %q", payload.Messages[1].Content)
	}
}

func TestRewriteWorkBuddyFingerprintPhrasesSkipsUntouchedParts(t *testing.T) {
	imageURL := "data:image/png;base64,iVBORw0KGgo="
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": []map[string]any{
				{"type": "text", "text": "You are an AI agent powered by DeepSeek Harness"},
				{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
			}},
			{"role": "user", "content": "You are an AI agent powered by DeepSeek Harness"},
			{"role": "tool", "content": "You are a coding agent powered by the x model"},
		},
		"tools": []map[string]any{{"type": "function", "function": map[string]any{"name": "read_image"}}},
	})
	got, changed := RewriteWorkBuddyFingerprintPhrases(body)
	if !changed {
		t.Fatal("system 的 text part 应被改写")
	}
	var payload map[string]json.RawMessage
	_ = json.Unmarshal(got, &payload)
	var msgs []map[string]json.RawMessage
	_ = json.Unmarshal(payload["messages"], &msgs)

	var parts []map[string]json.RawMessage
	_ = json.Unmarshal(msgs[0]["content"], &parts)
	var text string
	_ = json.Unmarshal(parts[0]["text"], &text)
	if strings.Contains(text, "powered by DeepSeek Harness") {
		t.Fatalf("text part 应被改写: %q", text)
	}
	var url string
	_ = json.Unmarshal(parts[1]["image_url"], &map[string]json.RawMessage{})
	var imageRaw map[string]json.RawMessage
	_ = json.Unmarshal(parts[1]["image_url"], &imageRaw)
	_ = json.Unmarshal(imageRaw["url"], &url)
	if url != imageURL {
		t.Fatalf("图片 part 不得改动: %q", url)
	}
	for _, index := range []int{1, 2} {
		var content string
		_ = json.Unmarshal(msgs[index]["content"], &content)
		if !strings.Contains(content, "powered by") {
			t.Fatalf("user/tool 消息不得改动: %q", content)
		}
	}
	var tools []map[string]json.RawMessage
	_ = json.Unmarshal(payload["tools"], &tools)
	if len(tools) != 1 {
		t.Fatal("工具定义不得改动")
	}
}

func TestRewriteWorkBuddyFingerprintPhrasesIdempotent(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "system", "content": realDSHSystemHead}},
	})
	first, ok := RewriteWorkBuddyFingerprintPhrases(body)
	if !ok {
		t.Fatal("首轮应命中")
	}
	second, again := RewriteWorkBuddyFingerprintPhrases(first)
	if again {
		t.Fatal("改写必须幂等：第二轮不应再有命中")
	}
	if !bytes.Equal(first, second) {
		t.Fatal("幂等要求二次调用逐字节相同")
	}
}

func TestRewriteWorkBuddyFingerprintPhrasesNoChange(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"role":"system","content":"You are a helpful assistant."}]}`,
		`{"messages":[{"role":"system","content":""}]}`,
		`{"messages":[{"role":"developer","content":"You are an AI agent powered by DeepSeek Harness"}]}`,
		`not-json`,
	} {
		got, changed := RewriteWorkBuddyFingerprintPhrases([]byte(body))
		if changed || !bytes.Equal(got, []byte(body)) {
			t.Fatalf("不应改动: %s", body)
		}
	}
}
