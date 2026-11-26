package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBridge(t *testing.T) {
	h := newBridge("secret", "example", func(_ context.Context, p string, emit func(string) error) (usage, error) {
		if p != "ping" {
			t.Fatalf("unexpected prompt %q", p)
		}
		return usage{Input: 2, Output: 1, Total: 3}, emit("OK")
	})
	for _, tt := range []struct {
		name, auth, body string
		status           int
		want             string
	}{
		{"auth", "", `{}`, 401, "invalid bridge token"},
		{"tools", "Bearer secret", `{"model":"example","tools":[]}`, 400, "plain-text"},
		{"wrong model", "Bearer secret", `{"model":"other"}`, 404, "model not available"},
		{"text", "Bearer secret", `{"model":"example","messages":[{"role":"user","content":"ping"}]}`, 200, `"content":"OK"`},
		{"stream", "Bearer secret", `{"model":"example","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"ping"}]}`, 200, "data: [DONE]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tt.body))
			r.Header.Set("Authorization", tt.auth)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestStreamDoesNotEndSuccessfullyAfterFailure(t *testing.T) {
	h := newBridge("secret", "example", func(_ context.Context, _ string, emit func(string) error) (usage, error) {
		_ = emit("partial")
		return usage{}, errors.New("private detail")
	})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"example","stream":true,"messages":[{"role":"user","content":"ping"}]}`))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, "agy did not finish") || strings.Contains(body, "[DONE]") || strings.Contains(body, "private detail") {
		t.Fatal(body)
	}
}

func TestReadEvents(t *testing.T) {
	for _, tt := range []struct {
		data, want string
		bad        bool
	}{
		{`{"event":"step_update","step_update":{"step_type":"agent_response","text_delta":"hi"}}` + "\n" + `{"event":"result","result":{"status":"SUCCESS","response":"hi"}}`, "hi", false},
		{`{"event":"result","result":{"status":"SUCCESS","response":"hi"}}`, "hi", false},
		{`{"event":"result","result":{"status":"ERROR","response":"private"}}`, "", true},
		{`{"event":"init"}`, "", true},
	} {
		var result strings.Builder
		_, err := readEvents(strings.NewReader(tt.data), func(s string) error { result.WriteString(s); return nil })
		if (err != nil) != tt.bad || result.String() != tt.want {
			t.Fatalf("%q: %q / %v", tt.data, result.String(), err)
		}
	}
}
