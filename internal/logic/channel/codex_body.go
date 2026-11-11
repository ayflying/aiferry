package channel

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
)

// ApplyCodexBody normalizes Responses requests for the official ChatGPT Codex
// backend, regardless of the gateway's generic passthrough/cache settings.
// Verified against openai/codex ResponsesApiRequest and build_responses_request:
// https://github.com/openai/codex/blob/rust-v0.100.0/codex-rs/codex-api/src/common.rs
// https://github.com/openai/codex/blob/rust-v0.100.0/codex-rs/core/src/client.rs
// This compatibility contract follows that fixed release (including instructions),
// not main's evolving wire format. Its request omits sampling/token-limit fields;
// do not pass ordinary OpenAI API controls to this backend.
func ApplyCodexBody(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, gerror.Wrap(err, "decode Codex request")
	}
	if payload == nil {
		return nil, gerror.New("Codex request must be an object")
	}
	for _, field := range []string{
		"max_output_tokens", "max_completion_tokens", "max_tokens", "temperature", "top_p",
		"frequency_penalty", "presence_penalty", "logprobs", "top_logprobs", "logit_bias",
		"seed", "stop", "n", "stream_options", "background", "truncation", "user",
		"metadata", "previous_response_id", "prompt_cache_retention", "prompt_cache_options",
		"prompt_cache_breakpoint", "prompt", "max_tool_calls", "safety_identifier", "inference_geo",
	} {
		delete(payload, field)
	}
	payload["stream"] = json.RawMessage("true")
	payload["store"] = json.RawMessage("false")
	if value, exists := payload["instructions"]; !exists || string(value) == "null" {
		payload["instructions"] = json.RawMessage(`""`)
	}
	// Codex serializes input as Vec<ResponseItem>, not the Responses API's string
	// shorthand (also used by the management model probe).
	var input string
	if value := payload["input"]; json.Unmarshal(value, &input) == nil {
		payload["input"], _ = json.Marshal([]any{map[string]any{
			"role": "user", "content": []any{map[string]any{"type": "input_text", "text": input}},
		}})
	}
	result, err := json.Marshal(payload)
	return result, gerror.Wrap(err, "encode Codex request")
}

// CodexHTTPClient forbids redirects entirely: even a same-host redirect could
// carry account headers to a non-Codex path. Clone rather than mutate a shared
// client's redirect policy, since other channels can use it concurrently.
func CodexHTTPClient(client *http.Client) *http.Client {
	guarded := *client
	guarded.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return gerror.New("Codex upstream redirects are not allowed")
	}
	return &guarded
}

// codexTestStreamError rejects HTTP 200 SSE failures and truncated probes. A
// successful TCP/HTTP exchange alone does not prove a model is callable.
func codexTestStreamError(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		event := gjson.Parse(payload)
		typ := event.Get("type").String()
		if typ == "error" || typ == "response.failed" || event.Get("response.error").IsObject() || event.Get("error").Exists() {
			for _, field := range []string{"response.error.message", "error.message", "message"} {
				if message := event.Get(field).String(); message != "" {
					return message
				}
			}
			return "Codex 上游流式请求失败"
		}
		if typ == "response.completed" {
			if status := event.Get("response.status").String(); status != "" && status != "completed" {
				return "Codex 上游响应未正常完成：" + status
			}
			return ""
		}
	}
	return "Codex 上游流式响应未正常结束（缺少完成事件）"
}

// codexTestEndpoints pins both automatic and explicit Chat probes to Responses.
// Non-chat protocols must not be guessed from model names or tried as fallback.
func codexTestEndpoints(endpoint string) ([]string, error) {
	switch endpoint {
	case "auto", "chat", "responses":
		return []string{"responses"}, nil
	default:
		return nil, gerror.New("Codex 官方渠道仅支持 Responses 和 Chat 文本请求")
	}
}
