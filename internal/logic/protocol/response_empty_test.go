package protocol

import (
	"testing"

	"github.com/tidwall/gjson"
)

// Codex 生产报错复现（2026-10-29 17:53，deepseek-flash）：
// error: created_at=... id="" model="" object="response" output=[] status="completed"
//
// 根因：上游 Chat 回包没有 choices（或 choices 为空），chatResponseToResponses
// 仍然合成「已成功完成、output 为空」的 Responses 响应，Codex 只能把它当结构化
// 错误原样打印。修复后必须返回可读的错误体，不能出现空 output 的 completed。
func TestChatResponseToResponsesRejectsEmptyCompletion(t *testing.T) {
	plan := PreferredChatCompletionsPlan(ResponsesEndpoint)
	for _, body := range []string{
		`{}`,
		`{"choices":[]}`,
		`{"id":"","model":"deepseek-flash","choices":[]}`,
		`{"choices":[{"index":0}]}`,
	} {
		converted := plan.ConvertResponse([]byte(body))
		if output := gjson.GetBytes(converted, "response.output"); output.Exists() {
			t.Fatalf("body %s: must not synthesize a response object, got %s", body, converted)
		}
		if status := gjson.GetBytes(converted, "response.status").String(); status == "completed" {
			t.Fatalf("body %s: empty completion must not report completed, got %s", body, converted)
		}
		message := gjson.GetBytes(converted, "error.message").String()
		if message != upstreamEmptyCompletionMessage {
			t.Fatalf("body %s: error message = %q, want the empty-completion reason", body, message)
		}
		if code := gjson.GetBytes(converted, "error.code").String(); code != "empty_completion" {
			t.Fatalf("body %s: error code = %q", body, code)
		}
	}
}

// 正常回包仍要完整转换，并且上游缺 id 时给出兜底 id（不能留空串）。
func TestChatResponseToResponsesKeepsOutputAndFillsID(t *testing.T) {
	plan := PreferredChatCompletionsPlan(ResponsesEndpoint)
	converted := plan.ConvertResponse([]byte(`{
	  "model":"deepseek-flash",
	  "choices":[{"index":0,"message":{"role":"assistant","content":"你好"}}]
	}`))
	if actual := gjson.GetBytes(converted, "output.0.type").String(); actual != "message" {
		t.Fatalf("output item type = %q, body=%s", actual, converted)
	}
	if actual := gjson.GetBytes(converted, "output.0.content.0.text").String(); actual != "你好" {
		t.Fatalf("output text = %q", actual)
	}
	if actual := gjson.GetBytes(converted, "status").String(); actual != "completed" {
		t.Fatalf("status = %q", actual)
	}
	if actual := gjson.GetBytes(converted, "id").String(); actual != generatedResponseID() {
		t.Fatalf("empty upstream id should fall back to %q, got %q", generatedResponseID(), actual)
	}
}
