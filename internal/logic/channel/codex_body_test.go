package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tidwall/gjson"
)

func TestCodexHTTPClientNeverFollowsRedirect(t *testing.T) {
	redirected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			redirected = true
			return
		}
		http.Redirect(w, r, "/outside-codex", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	original := server.Client()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer synthetic-test-token")
	req.Header.Set("ChatGPT-Account-Id", "synthetic-account")
	resp, err := CodexHTTPClient(original).Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || redirected || original.CheckRedirect != nil {
		t.Fatalf("redirect safety: err=%v redirected=%v shared client mutated=%v", err, redirected, original.CheckRedirect != nil)
	}
}

func TestApplyCodexBody(t *testing.T) {
	body, err := ApplyCodexBody([]byte(`{"model":"future-model","input":"hello","stream":false,"store":true,"temperature":0.7,"top_p":1,"max_output_tokens":16,"previous_response_id":"resp_old","reasoning":{"effort":"high"},"tools":[{"type":"function","name":"f"}],"prompt_cache_key":"stable"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"temperature", "top_p", "max_output_tokens", "previous_response_id"} {
		if gjson.GetBytes(body, field).Exists() {
			t.Fatalf("unsupported %s retained: %s", field, body)
		}
	}
	if !gjson.GetBytes(body, "stream").Bool() || gjson.GetBytes(body, "store").Bool() || !gjson.GetBytes(body, "instructions").Exists() || gjson.GetBytes(body, "instructions").String() != "" {
		t.Fatalf("invalid required fields: %s", body)
	}
	if gjson.GetBytes(body, "input.0.content.0.text").String() != "hello" || gjson.GetBytes(body, "reasoning.effort").String() != "high" || gjson.GetBytes(body, "tools.0.name").String() != "f" || gjson.GetBytes(body, "prompt_cache_key").String() != "stable" {
		t.Fatalf("lost input or supported fields: %s", body)
	}
	body, err = ApplyCodexBody([]byte(`{"instructions":"keep me","input":[]}`))
	if err != nil || gjson.GetBytes(body, "instructions").String() != "keep me" {
		t.Fatalf("instructions lost: %s %v", body, err)
	}
}

func TestCodexProbeStreamCompletion(t *testing.T) {
	for _, body := range []string{
		`data: {"type":"response.failed","response":{"error":{"message":"quota exceeded"}}}`,
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		`data: {"type":"response.completed","response":{"status":"incomplete"}}`,
	} {
		if codexTestStreamError([]byte(body)) == "" {
			t.Fatalf("failed/truncated probe accepted: %s", body)
		}
	}
	if message := codexTestStreamError([]byte(`data: {"type":"response.completed","response":{"status":"completed"}}`)); message != "" {
		t.Fatalf("completed probe rejected: %s", message)
	}
}

func TestCodexModelProbeUsesSameBodyRules(t *testing.T) {
	for _, endpoint := range []string{"auto", "chat", "responses"} {
		endpoints, err := codexTestEndpoints(endpoint)
		if err != nil || len(endpoints) != 1 || endpoints[0] != "responses" {
			t.Fatalf("endpoints = %v, %v", endpoints, err)
		}
	}
	if _, err := codexTestEndpoints("embeddings"); err == nil {
		t.Fatal("embedding probe must fail locally")
	}
	req, forced, err := buildTestRequest(context.Background(), "https://chatgpt.com/backend-api/codex/responses", "codex", map[string]any{"model": "any-model", "input": "OK", "max_output_tokens": 16}, AdvancedConfig{}, "probe")
	if err != nil {
		t.Fatal(err)
	}
	defer req.Body.Close()
	body, _ := io.ReadAll(req.Body)
	if !forced || !gjson.GetBytes(body, "stream").Bool() || gjson.GetBytes(body, "store").Bool() || !gjson.GetBytes(body, "instructions").Exists() || gjson.GetBytes(body, "max_output_tokens").Exists() || !gjson.GetBytes(body, "input").IsArray() {
		t.Fatalf("invalid probe: %s", body)
	}
}
