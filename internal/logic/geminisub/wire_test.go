package geminisub

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestBuildRequest(t *testing.T) {
	body, err := BuildRequest(fixture(t, "chat-request.json"), "gemini-example", "example-project")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"model": "gemini-example", "project": "example-project", "requestType": "agent",
		"request.systemInstruction.role": "user", "request.systemInstruction.parts.0.text": "Use short sentences.",
		"request.systemInstruction.parts.1.text": "Answer in Chinese.",
		"request.contents.0.parts.1.text":        "，世界", "request.contents.1.role": "model",
		"request.contents.2.parts.0.text": "继续。", "request.generationConfig.temperature": "0",
		"request.generationConfig.topP": "0.8", "request.generationConfig.maxOutputTokens": "256",
		"request.generationConfig.stopSequences.0": "END",
	}
	checkPaths(t, body, checks)
	if id := gjson.GetBytes(body, "requestId").Str; !strings.HasPrefix(id, "agent-") || len(id) != 30 {
		t.Fatalf("unexpected request ID: %q", id)
	}
	if gjson.GetBytes(body, "userAgent").Exists() {
		t.Fatal("request must not impersonate an official client")
	}
}

func TestBuildRequestTextOnly(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test"}}]}]}`,
		`{"messages":[{"role":"assistant","content":"x","tool_calls":[{}]}]}`,
		`{"messages":[{"role":"user","content":"x"}],"tools":[{}]}`,
		`{"messages":[{"role":"system","content":"x"}]}`,
		`{"messages":[{"role":"user","content":null}]}`,
		`{"messages":[{"role":"user","content":"x"}],"n":2}`,
	} {
		if _, err := BuildRequest([]byte(body), "m", "p"); err == nil {
			t.Errorf("expected unsupported-input error for %s", body)
		}
	}
	body, err := BuildRequest([]byte(`{"messages":[{"role":"assistant","content":"in progress"}]}`), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	checkPaths(t, body, map[string]string{"request.contents.1.role": "user", "request.contents.1.parts.0.text": "Continue"})
}

func TestConvertStream(t *testing.T) {
	var output bytes.Buffer
	usage, err := ConvertStream(&output, bytes.NewReader(fixture(t, "response.sse")), "public-model", "chatcmpl-fixture")
	if err != nil {
		t.Fatal(err)
	}
	checkUsage(t, usage)
	if !strings.HasSuffix(output.String(), "data: [DONE]\n\n") {
		t.Fatalf("missing stream terminator: %s", output.String())
	}
	frames := strings.Split(strings.TrimSuffix(output.String(), "\n\n"), "\n\n")
	want := []map[string]string{
		{"choices.0.delta.role": "assistant"},
		{"choices.0.delta.reasoning_content": "想一下"},
		{"choices.0.delta.content": "你"},
		{"choices.0.delta.content": "好"},
		{"choices.0.finish_reason": "length"},
		{"choices": "[]", "usage.prompt_tokens_details.cached_tokens": "6", "usage.completion_tokens_details.reasoning_tokens": "2"},
	}
	if len(frames) != len(want)+1 {
		t.Fatalf("unexpected stream frames: %s", output.String())
	}
	for i, paths := range want {
		paths["object"], paths["model"], paths["id"] = "chat.completion.chunk", "public-model", "chatcmpl-fixture"
		checkPaths(t, []byte(strings.TrimPrefix(frames[i], "data: ")), paths)
	}
}

func TestConvertResponse(t *testing.T) {
	body, usage, err := ConvertResponse(bytes.NewReader(fixture(t, "response.sse")), "public-model", "chatcmpl-fixture")
	if err != nil {
		t.Fatal(err)
	}
	checkUsage(t, usage)
	checkPaths(t, body, map[string]string{
		"object": "chat.completion", "id": "chatcmpl-fixture", "model": "public-model",
		"choices.0.message.role": "assistant", "choices.0.message.content": "你好",
		"choices.0.message.reasoning_content": "想一下", "choices.0.finish_reason": "length",
		"usage.prompt_tokens": "20", "usage.completion_tokens": "5", "usage.total_tokens": "25",
	})
}

func TestConvertResponseFormats(t *testing.T) {
	text := `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`
	for _, input := range []string{text, `[ ` + text + ` ]`, "data: " + text + "\r\ndata: [DONE]\r\n"} {
		body, _, err := ConvertResponse(strings.NewReader(input), "m", "id")
		if err != nil {
			t.Fatal(err)
		}
		checkPaths(t, body, map[string]string{"choices.0.message.content": "ok", "choices.0.finish_reason": "stop"})
	}
	body, _, err := ConvertResponse(strings.NewReader(`{"response":{"promptFeedback":{"blockReason":"SAFETY"}}}`), "m", "id")
	if err != nil {
		t.Fatal(err)
	}
	checkPaths(t, body, map[string]string{"choices.0.finish_reason": "content_filter"})
}

func TestConvertStreamErrorsDoNotWriteDone(t *testing.T) {
	for _, input := range []string{
		"data: {oops}\n\n",
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n",
		`{"response":{"error":{"code":429,"message":"must not expose upstream body"}}}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"foo"}}]},"finishReason":"STOP"}]}`,
	} {
		var output bytes.Buffer
		_, err := ConvertStream(&output, strings.NewReader(input), "m", "id")
		if err == nil || strings.Contains(err.Error(), "must not expose upstream body") || strings.Contains(output.String(), "[DONE]") {
			t.Errorf("unexpected result: output=%q, err=%v", output.String(), err)
		}
	}
	expected := errors.New("closed writer")
	_, err := ConvertStream(failingWriter{expected}, bytes.NewReader(fixture(t, "response.sse")), "m", "id")
	if !errors.Is(err, expected) {
		t.Fatalf("writer error not propagated: %v", err)
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func checkUsage(t *testing.T, got Usage) {
	t.Helper()
	if want := (Usage{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25, CachedTokens: 6}); got != want {
		t.Fatalf("got usage %+v; want %+v", got, want)
	}
}

func checkPaths(t *testing.T, body []byte, checks map[string]string) {
	t.Helper()
	for path, want := range checks {
		got := gjson.GetBytes(body, path)
		if !got.Exists() || got.String() != want {
			t.Errorf("%s: got %q; want %q; body: %s", path, got.String(), want, body)
		}
	}
}
