package channel

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGeminiOAuthPKCEAndCallbackConfig(t *testing.T) {
	cfg := geminiOAuthConfig{clientID: "operator-client", clientSecret: "operator-secret", redirectURI: "https://aiferry.example" + GeminiOAuthCallbackPath}
	if err := cfg.validateRedirectURI(); err != nil {
		t.Fatal(err)
	}
	verifier, challenge, err := geminiPKCE()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(verifier))
	if challenge != base64.RawURLEncoding.EncodeToString(hash[:]) || len(verifier) != 43 {
		t.Fatal("invalid S256 challenge")
	}
	u, err := url.Parse(cfg.authorizationURL("state-fixture", challenge, false))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("state") != "state-fixture" || q.Get("client_id") != cfg.clientID || q.Get("redirect_uri") != cfg.redirectURI || q.Get("access_type") != "offline" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != challenge || strings.Contains(u.String(), cfg.clientSecret) || strings.Contains(u.String(), verifier) {
		t.Fatal("authorization URL is incorrect or leaks a secret")
	}
	for _, invalid := range []string{
		"http://aiferry.example" + GeminiOAuthCallbackPath,
		"https://someone@aiferry.example" + GeminiOAuthCallbackPath,
		"https://aiferry.example" + GeminiOAuthCallbackPath + "?",
		"https://aiferry.example" + GeminiOAuthCallbackPath + "#",
		"https://aiferry.example/incorrect",
	} {
		cfg.redirectURI = invalid
		if cfg.validateRedirectURI() == nil {
			t.Errorf("accepted %s", invalid)
		}
	}
}

func TestGeminiOAuthSealsAndBindsCredentials(t *testing.T) {
	cfg := geminiOAuthConfig{clientID: "operator-client", clientSecret: "operator-secret"}
	current := geminiSubscriptionCredential{Subject: "test-account", AccessToken: "access-fixture", RefreshToken: "refresh-fixture", ExpiresAt: 123}
	aad := geminiCredentialAAD(1, geminiAccountHash(current.Subject))
	value, err := cfg.seal(aad, current)
	if err != nil {
		t.Fatal(err)
	}
	same, err := cfg.seal(aad, current)
	if err != nil || same == value || strings.Contains(value, current.RefreshToken) || strings.Contains(value, cfg.clientSecret) {
		t.Fatal("expected randomized sealed payload")
	}
	var opened geminiSubscriptionCredential
	if err = cfg.open(aad, value, &opened); err != nil || opened != current {
		t.Fatal("could not restore sealed credential")
	}
	if cfg.open(geminiCredentialAAD(2, geminiAccountHash(current.Subject)), value, &opened) == nil || cfg.open("login:state", value, &opened) == nil || cfg.open(aad, value[:len(value)/2], &opened) == nil {
		t.Fatal("accepted substituted or modified ciphertext")
	}
	cfg.clientSecret = "rotated-operator-secret"
	if cfg.open(aad, value, &opened) == nil {
		t.Fatal("accepted seal with wrong key")
	}
	now := time.Unix(200, 0)
	updated := mergeGeminiRefresh(current, geminiOAuthTokens{AccessToken: "updated", ExpiresIn: 3600}, now)
	if updated.RefreshToken != current.RefreshToken || updated.Subject != current.Subject || updated.ExpiresAt != 3800 || updated.AccessToken != "updated" {
		t.Fatal("refresh lost account or offline token")
	}
	if mergeGeminiRefresh(updated, geminiOAuthTokens{AccessToken: "rotated", RefreshToken: "rotated-offline", ExpiresIn: 3600}, now).RefreshToken != "rotated-offline" {
		t.Fatal("refresh did not retain rotated refresh token")
	}
}

func TestGeminiOAuthCredentialRequestsNeverRedirectOrExposeErrors(t *testing.T) {
	for _, redirectStatus := range []int{301, 302, 307, 308} {
		for _, bearer := range []bool{false, true} {
			count := 0
			client := &http.Client{Transport: codexRoundTrip(func(r *http.Request) (*http.Response, error) {
				count++
				if bearer {
					if r.URL.String() != geminiOAuthUserInfoURL || r.Header.Get("Authorization") != "Bearer access-fixture" || r.Method != http.MethodGet {
						t.Error("unexpected userinfo request")
					}
				} else {
					if r.URL.String() != geminiOAuthTokenURL || r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("client_secret") != "operator-secret" || r.Form.Get("refresh_token") != "refresh-fixture" {
						t.Error("unexpected token request")
					}
				}
				return &http.Response{StatusCode: redirectStatus, Header: http.Header{"Location": {"https://unexpected.example/"}}, Body: io.NopCloser(strings.NewReader("secret-response")), Request: r}, nil
			})}
			var err error
			if bearer {
				_, err = geminiOAuthSubject(context.Background(), client, "access-fixture")
			} else {
				_, err = exchangeGeminiOAuthTokens(context.Background(), client, geminiOAuthConfig{clientID: "operator", clientSecret: "operator-secret"}, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"refresh-fixture"}})
			}
			if err == nil || count != 1 || strings.Contains(err.Error(), "secret-response") || client.CheckRedirect != nil {
				t.Fatalf("HTTP %d: unsafe redirect handling", redirectStatus)
			}
		}
	}
	client := &http.Client{Transport: codexRoundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-proxy-password") })}
	if _, err := geminiOAuthSubject(context.Background(), client, "access-fixture"); err == nil || strings.Contains(err.Error(), "secret-proxy-password") {
		t.Fatal("transport error leaked details")
	}
}
