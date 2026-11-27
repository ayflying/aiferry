package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// 登录端点只负责 HTTP 形状，OAuth 细节都在 oauth.go。与其他 /v1 路径一样，
// 它们先经过桥接令牌认证：认证用哪把令牌，登录态就写进哪个账号的 HOME。
// 这样多账号场景下「给谁登录」是明确的，不依赖额外的账号参数。

// loginStartResponse 交给管理端渲染：authUrl 由管理员在浏览器打开。
type loginStartResponse struct {
	State     string `json:"state"`
	AuthURL   string `json:"authUrl"`
	ExpiresIn int    `json:"expiresIn"`
}

// loginStart 生成一次授权：返回 state（随后兑现用）与 Google 授权地址。
func (b *bridge) loginStart(w http.ResponseWriter, r *http.Request) {
	acc, _ := currentAccount(r.Context())
	state, authURL, err := b.logins.start(acc.name)
	if err != nil {
		fail(w, 500, "failed to start authorization")
		return
	}
	reply(w, loginStartResponse{State: state, AuthURL: authURL, ExpiresIn: int(loginSessionTTL.Seconds())})
}

// loginComplete 兑现一次授权：管理员把浏览器回调地址（或其中的授权码）粘回来，
// 这里换取令牌并按官方 CLI 的格式写进该账号 HOME。
func (b *bridge) loginComplete(w http.ResponseWriter, r *http.Request) {
	acc, requestProxy := currentAccount(r.Context())
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		fail(w, 400, "invalid request body")
		return
	}
	var input struct {
		State string `json:"state"`
		Input string `json:"input"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		fail(w, 400, "invalid request body")
		return
	}
	state := strings.TrimSpace(input.State)
	if state == "" {
		fail(w, 400, "state is required")
		return
	}
	verifier, ok := b.logins.take(state, acc.name)
	if !ok {
		fail(w, 400, "authorization session expired or belongs to another account; start again")
		return
	}
	code, err := parseLoginInput(input.Input)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	// 代理优先级与对话请求一致：请求头 > 账号固定配置 > 编排全局代理。
	proxy := requestProxy
	if proxy == "" {
		proxy = acc.proxy
	}
	if proxy == "" {
		proxy = globalProxy()
	}
	document, err := exchangeAntigravityCode(r.Context(), code, verifier, proxy)
	if err != nil {
		log.Printf("[agy-bridge] authorization failed for account %s: %v", acc.name, err)
		fail(w, 502, err.Error())
		return
	}
	if err := writeAntigravityToken(acc.home, document); err != nil {
		log.Printf("[agy-bridge] failed to persist token for account %s: %v", acc.name, err)
		fail(w, 500, "failed to write the token file")
		return
	}
	email := antigravityAccountEmail(r.Context(), document.Token.AccessToken, proxy)
	if email != "" {
		log.Printf("[agy-bridge] account %s authorized as %s", acc.name, email)
	} else {
		log.Printf("[agy-bridge] account %s authorized", acc.name)
	}
	reply(w, map[string]any{"account": acc.name, "email": email, "expiresAt": document.Token.Expiry})
}
