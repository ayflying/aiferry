package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type runner func(context.Context, string, func(string) error) (usage, error)

type bridge struct {
	token [32]byte
	model string
	run   runner
	slot  chan struct{}
}

func newBridge(token, model string, run runner) http.Handler {
	b := &bridge{sha256.Sum256([]byte("Bearer " + token)), model, run, make(chan struct{}, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", b.models)
	mux.HandleFunc("POST /v1/chat/completions", b.chat)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(b.token[:], actual[:]) != 1 {
			fail(w, 401, "invalid bridge token")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (b *bridge) models(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]any{"object": "list", "data": []any{map[string]any{"id": b.model, "object": "model", "owned_by": "agy"}}})
}

type chatRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
	// AiFerry 模型探测固定附带此字段；CLI 无对应输出 token 上限，不能将其宣传为硬限制。
	ProbeTokenHint *int `json:"max_completion_tokens,omitempty"`
	StreamOptions  *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func (p chatRequest) prompt() (string, bool) {
	if len(p.Messages) == 0 || len(p.Messages) > 64 || p.Messages[len(p.Messages)-1].Role != "user" {
		return "", false
	}
	if len(p.Messages) == 1 {
		return p.Messages[0].Content, strings.TrimSpace(p.Messages[0].Content) != ""
	}
	for _, m := range p.Messages {
		switch m.Role {
		case "system", "developer", "user", "assistant":
		default:
			return "", false
		}
		if strings.TrimSpace(m.Content) == "" {
			return "", false
		}
	}
	data, err := json.Marshal(p.Messages)
	return "Continue the following text-only conversation. Reply to the final user message. Do not use tools.\n" + string(data), err == nil
}

func (b *bridge) chat(w http.ResponseWriter, r *http.Request) {
	var p chatRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil || dec.Decode(new(any)) != io.EOF {
		fail(w, 400, "only model, stream and plain-text messages are supported")
		return
	}
	if p.Model != b.model {
		fail(w, 404, "model not available on this bridge")
		return
	}
	prompt, ok := p.prompt()
	if !ok {
		fail(w, 400, "expected plain-text conversation ending in a user message")
		return
	}
	select {
	case b.slot <- struct{}{}:
		defer func() { <-b.slot }()
	default:
		fail(w, 429, "bridge already processing a request")
		return
	}
	now := time.Now()
	id := fmt.Sprintf("chatcmpl-agy-%x", now.UnixNano())
	packet := func(object string, choice map[string]any) map[string]any {
		return map[string]any{"id": id, "object": object, "created": now.Unix(), "model": b.model, "choices": []any{choice}}
	}
	if p.Stream {
		b.stream(w, r, prompt, packet)
		return
	}
	var text strings.Builder
	u, err := b.run(r.Context(), prompt, func(s string) error {
		if text.Len()+len(s) > 2<<20 {
			return fmt.Errorf("response too large")
		}
		text.WriteString(s)
		return nil
	})
	if err != nil {
		fail(w, 502, "agy failed to return a response")
		return
	}
	res := packet("chat.completion", map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": text.String()}})
	res["usage"] = usageJSON(u)
	reply(w, res)
}

func (b *bridge) stream(w http.ResponseWriter, r *http.Request, prompt string, packet func(string, map[string]any) map[string]any) {
	flush, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "streaming not available")
		return
	}
	started := false
	send := func(v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if !started {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Accel-Buffering", "no")
			started = true
		}
		if _, err = fmt.Fprintf(w, "data: %s\n\n", data); err == nil {
			flush.Flush()
		}
		return err
	}
	chunk := func(delta map[string]string, finish any) map[string]any {
		return packet("chat.completion.chunk", map[string]any{"index": 0, "delta": delta, "finish_reason": finish})
	}
	u, err := b.run(r.Context(), prompt, func(s string) error {
		if !started {
			if err := send(chunk(map[string]string{"role": "assistant"}, nil)); err != nil {
				return err
			}
		}
		return send(chunk(map[string]string{"content": s}, nil))
	})
	if err != nil {
		if !started {
			fail(w, 502, "agy failed to return a response")
		} else {
			_ = send(errorBody("agy did not finish the response"))
		}
		return
	}
	last := chunk(map[string]string{}, "stop")
	last["usage"] = usageJSON(u)
	if send(last) == nil {
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flush.Flush()
	}
}

func usageJSON(u usage) map[string]int {
	return map[string]int{"prompt_tokens": u.Input, "completion_tokens": u.Output, "total_tokens": u.Total}
}
func errorBody(message string) map[string]any {
	return map[string]any{"error": map[string]string{"message": message, "type": "agy_bridge_error"}}
}
func fail(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorBody(message))
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
