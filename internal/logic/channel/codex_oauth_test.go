package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type codexRoundTrip func(*http.Request) (*http.Response, error)

func (f codexRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func codexTestJWT(claims string) string {
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
}
func codexFixtureCredential() codexCredential {
	return codexCredential{ChannelID: 7, AccountID: "acct-fixture", AccessToken: "access-fixture", RefreshToken: "refresh-fixture", ExpiresAt: time.Now().Add(time.Hour).Unix()}
}

func TestCodexTokenClaimAndRefresh(t *testing.T) {
	id := codexTestJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-fixture"}}`)
	access := codexTestJWT(`{"exp":2000000000}`)
	credential, err := newCodexCredential(codexTokenResponse{IDToken: id, AccessToken: access, RefreshToken: "refresh"})
	if err != nil || credential.AccountID != "acct-fixture" || credential.ExpiresAt != 2000000000 {
		t.Fatalf("credential: %+v %v", credential, err)
	}
	for _, bad := range []string{"not-jwt", codexTestJWT(`{"sub":"not-account"}`)} {
		if _, err = codexTokenAccount(bad); err == nil {
			t.Fatal("invalid account accepted")
		}
	}
	credential.ChannelID = 7
	refreshed, err := mergeCodexTokenRefresh(credential, codexTokenResponse{AccessToken: access})
	if err != nil || refreshed.RefreshToken != "refresh" || refreshed.ChannelID != 7 {
		t.Fatalf("refresh dropped fields: %+v %v", refreshed, err)
	}
	refreshed, err = mergeCodexTokenRefresh(credential, codexTokenResponse{AccessToken: access, RefreshToken: "rotated"})
	if err != nil || refreshed.RefreshToken != "rotated" {
		t.Fatal("refresh rotation not retained")
	}
	mismatch := codexTestJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"another"}}`)
	if _, err = mergeCodexTokenRefresh(credential, codexTokenResponse{AccessToken: access, IDToken: mismatch}); err == nil {
		t.Fatal("account mismatch accepted")
	}
	if _, err = mergeCodexTokenRefresh(credential, codexTokenResponse{}); err == nil {
		t.Fatal("missing access token accepted")
	}
	if _, err = decodeCodexCredential(codexCredentialPrefix + `{}`); err == nil {
		t.Fatal("empty envelope accepted")
	}
	if _, err = decodeCodexCredential(encodeCodexCredential(codexFixtureCredential())); err != nil {
		t.Fatal(err)
	}
}

func TestCodexAuthHeadersAndDestination(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{}`)
	credential := codexFixtureCredential()
	credential.Seal, _ = secrets.Encrypt("codex:7:acct-fixture")
	cipher, err := secrets.Encrypt(encodeCodexCredential(credential))
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://chatgpt.com/backend-api/codex/responses", "https://chatgpt.com/backend-api/wham/usage"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if err = svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: cipher}); err != nil {
			t.Fatal(err)
		}
		if req.Header.Get("Authorization") != "Bearer access-fixture" || req.Header.Get("ChatGPT-Account-Id") != "acct-fixture" {
			t.Fatal("OAuth envelope not unpacked")
		}
		if strings.Contains(req.Header.Get("Authorization"), "refresh") {
			t.Fatal("refresh token leaked")
		}
	}
	for _, endpoint := range []string{"http://chatgpt.com/backend-api/codex/responses", "https://evil.example/backend-api/codex/responses", "https://chatgpt.com.evil.example/backend-api/codex/responses", "https://chatgpt.com:443/backend-api/codex/responses", "https://chatgpt.com/elsewhere"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if err = svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: cipher}); err == nil {
			t.Fatalf("destination accepted: %s", endpoint)
		}
	}
}

func TestCodexIntervalShapes(t *testing.T) {
	for _, input := range []string{`5`, `"5"`} {
		var interval codexSeconds
		if err := json.Unmarshal([]byte(input), &interval); err != nil || interval != 5 {
			t.Fatalf("interval %s: %d %v", input, interval, err)
		}
	}
}

func TestCodexCodeExchangeAndNoRedirect(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: codexRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "auth.openai.com" || req.URL.Path != "/oauth/token" || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("wrong exchange target")
		}
		body, _ := io.ReadAll(req.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("code") != "one-use" || form.Get("code_verifier") != "verifier" || form.Get("client_id") != codexClientID {
			t.Fatal("wrong PKCE exchange")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example/stolen"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	_, err := exchangeCodexTokens(context.Background(), client, url.Values{"grant_type": {"authorization_code"}, "client_id": {codexClientID}, "code": {"one-use"}, "code_verifier": {"verifier"}})
	if err == nil || calls != 1 {
		t.Fatalf("redirect followed: calls=%d err=%v", calls, err)
	}
	if strings.Contains(err.Error(), "one-use") || strings.Contains(err.Error(), "verifier") {
		t.Fatal("OAuth secrets leaked in error")
	}
}
