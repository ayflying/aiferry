package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// bridge 是 OpenAI 兼容网关：按 Authorization 里的桥接令牌识别账号，
// 再以该账号的登录状态调用官方 agy。每个账号同一时刻只处理一个请求。
type bridge struct {
	// mu 保护 accounts 与 slots：首次登录允许按需登记新账号，二者会同时增长。
	mu       sync.RWMutex
	accounts []account
	model    string
	run      func(account, string) runner
	usage    func(account, string) func(context.Context) (usagePayload, error)
	cli      *cliManager
	slots    map[string]chan struct{}
	// logins 保存在内存里的授权会话，供 /v1/login/* 使用。
	logins *loginSessions
	// stateDir 与 autoAccount 只在「首次登录自动登记账号」时用到。
	stateDir    string
	autoAccount bool
}

func newBridge(accounts []account, model string, run func(account, string) runner, usage func(account, string) func(context.Context) (usagePayload, error), cli *cliManager, stateDir string, autoAccount bool) http.Handler {
	b := &bridge{accounts: accounts, model: model, run: run, usage: usage, cli: cli, slots: make(map[string]chan struct{}, len(accounts)), logins: newLoginSessions(), stateDir: stateDir, autoAccount: autoAccount}
	for _, acc := range accounts {
		b.slots[acc.name] = make(chan struct{}, 1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", b.models)
	mux.HandleFunc("GET /v1/quota", b.quota)
	mux.HandleFunc("GET /v1/version", b.version)
	mux.HandleFunc("POST /v1/chat/completions", b.chat)
	mux.HandleFunc("POST /v1/login/start", b.loginStart)
	mux.HandleFunc("POST /v1/login/complete", b.loginComplete)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		acc, ok := b.resolveAccount(authorization)
		if !ok {
			// /v1/login/start 是「新渠道第一次登录」的入口：AiFerry 在渠道还没有
			// 令牌时会自己生成一把再调这里，桥接据此登记账号，用户不必手工准备令牌。
			// 只在这一个端点、且显式开启时才放行，其余端点仍然只认已存在的令牌。
			if b.autoAccount && r.URL.Path == "/v1/login/start" {
				acc, ok = b.registerAccount(authorization)
			}
			if !ok {
				fail(w, 401, "invalid bridge token")
				return
			}
		}
		// 令牌认证通过后才读取代理头；账号固定代理为兜底，请求头优先。
		proxy := acc.proxy
		if fromHeader := r.Header.Get(proxyHeader); fromHeader != "" {
			proxy = fromHeader
		}
		mux.ServeHTTP(w, r.WithContext(withAccount(r.Context(), acc, proxy)))
	})
}

// resolveAccount 加读锁查账号，避免与自动登记并发读写切片。
func (b *bridge) resolveAccount(authorization string) (account, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return resolve(b.accounts, authorization)
}

// registerAccount 为一把未知令牌建立账号目录并加入运行期账号表。
// 落盘失败或无卷可写时返回 false，调用方按无效令牌处理。
func (b *bridge) registerAccount(authorization string) (account, bool) {
	acc, err := createAccountDir(b.stateDir, authorization)
	if err != nil {
		return account{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.slots[acc.name]; !exists {
		b.accounts = append(b.accounts, acc)
		b.slots[acc.name] = make(chan struct{}, 1)
	}
	return acc, true
}

// slot 取账号的并发槽位；账号在运行期可能新增，必须加锁读取。
func (b *bridge) slot(name string) chan struct{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.slots[name]
}

type accountKey struct{}

// withAccount 把已认证的账号与本次请求的代理放进上下文，供各处理器取用。
func withAccount(ctx context.Context, acc account, proxy string) context.Context {
	return context.WithValue(ctx, accountKey{}, struct {
		account account
		proxy   string
	}{acc, proxy})
}

func currentAccount(ctx context.Context) (account, string) {
	value, _ := ctx.Value(accountKey{}).(struct {
		account account
		proxy   string
	})
	return value.account, value.proxy
}

func (b *bridge) models(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]any{"object": "list", "data": []any{map[string]any{"id": b.model, "object": "model", "owned_by": "agy"}}})
}

// version 回显桥接与卷内官方 CLI 的版本号，排查「协议不兼容」时先看这里。
func (b *bridge) version(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]string{"bridge": bridgeVersion, "agy": b.cli.currentVersion()})
}

// quota 用官方 CLI 的 /usage 查询该账号的订阅额度，输出 OpenCode Go 同款形状。
// 不经过对话槽位串行化：额度查询不占模型并发，但同一账号仍限一个并发查询。
func (b *bridge) quota(w http.ResponseWriter, r *http.Request) {
	acc, proxy := currentAccount(r.Context())
	select {
	case b.slot(acc.name) <- struct{}{}:
		defer func() { <-b.slot(acc.name) }()
	default:
		fail(w, 429, "bridge already processing a request for this account")
		return
	}
	payload, err := b.usage(acc, proxy)(r.Context())
	if err != nil {
		fail(w, 502, "agy failed to report subscription usage")
		return
	}
	reply(w, payload)
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
	acc, proxy := currentAccount(r.Context())
	select {
	case b.slot(acc.name) <- struct{}{}:
		defer func() { <-b.slot(acc.name) }()
	default:
		fail(w, 429, "bridge already processing a request for this account")
		return
	}
	now := time.Now()
	id := fmt.Sprintf("chatcmpl-agy-%x", now.UnixNano())
	packet := func(object string, choice map[string]any) map[string]any {
		return map[string]any{"id": id, "object": object, "created": now.Unix(), "model": b.model, "choices": []any{choice}}
	}
	if p.Stream {
		b.stream(w, r, b.run(acc, proxy), prompt, packet)
		return
	}
	var text strings.Builder
	u, err := b.run(acc, proxy)(r.Context(), prompt, func(s string) error {
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

func (b *bridge) stream(w http.ResponseWriter, r *http.Request, run runner, prompt string, packet func(string, map[string]any) map[string]any) {
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
	u, err := run(r.Context(), prompt, func(s string) error {
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
