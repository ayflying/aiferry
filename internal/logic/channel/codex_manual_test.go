package channel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func manualCodexTestToken(expiry int64) string {
	return codexTestJWT(fmt.Sprintf(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-manual"},"exp":%d}`, expiry))
}

func TestManualCodexCredentialValidation(t *testing.T) {
	expiry := time.Now().Add(time.Hour).Unix()
	token := manualCodexTestToken(expiry)
	credential, err := manualCodexCredential("  " + token + "\n")
	if err != nil || credential.AccessToken != token || credential.AccountID != "acct-manual" || credential.ExpiresAt != expiry || credential.RefreshToken != "" {
		t.Fatalf("manual token claims not preserved: %v", err)
	}
	for name, value := range map[string]string{
		"empty": "", "api key": "sk-fixture-not-an-access-token",
		"internal envelope": encodeCodexCredential(codexFixtureCredential()),
		"malformed JWT":     "not-a-jwt", "malformed claims": "header.not-base64.signature",
		"unknown account": codexTestJWT(fmt.Sprintf(`{"sub":"not-a-chatgpt-account","exp":%d}`, expiry)),
		"empty account":   codexTestJWT(fmt.Sprintf(`{"https://api.openai.com/auth":{"chatgpt_account_id":" "},"exp":%d}`, expiry)),
		"missing expiry":  codexTestJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-manual"}}`),
		"string expiry":   codexTestJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-manual"},"exp":"2000000000"}`),
		"expired":         manualCodexTestToken(time.Now().Add(-time.Hour).Unix()),
		"expires now":     manualCodexTestToken(time.Now().Unix()),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := manualCodexCredential(value); err == nil {
				t.Fatal("invalid manual token accepted")
			}
		})
	}
}

func TestManualCodexCredentialSealAndAuth(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{}`)
	token := manualCodexTestToken(time.Now().Add(time.Hour).Unix())
	data, err := svc.manualCodexCredentialData(42, token)
	if err != nil {
		t.Fatal(err)
	}
	cipher, ok := data.ApiKeyCipher.(string)
	if !ok || cipher == "" || strings.Contains(cipher, token) {
		t.Fatal("manual token is not encrypted")
	}
	plain, err := secrets.Decrypt(cipher)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := decodeCodexCredential(plain)
	if err != nil || credential.ChannelID != 42 || credential.AccountID != "acct-manual" || credential.RefreshToken != "" || credential.AccessToken != token {
		t.Fatalf("manual envelope lost claims: %v", err)
	}
	seal, err := secrets.Decrypt(credential.Seal)
	if err != nil || seal != "codex:42:acct-manual" || data.KeyHash != codexAccountHash("acct-manual") {
		t.Fatal("manual envelope source/channel not sealed")
	}
	for _, endpoint := range []string{"https://chatgpt.com/backend-api/codex/responses", "https://chatgpt.com/backend-api/codex/models?client_version=0.101.0", "https://chatgpt.com/backend-api/wham/usage"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if err := svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: cipher}); err != nil {
			t.Fatal(err)
		}
		if req.Header.Get("Authorization") != "Bearer "+token || req.Header.Get("ChatGPT-Account-Id") != "acct-manual" {
			t.Fatal("manual envelope auth headers missing")
		}
		if strings.Contains(req.Header.Get("Authorization"), codexCredentialPrefix) {
			t.Fatal("internal envelope leaked upstream")
		}
	}
	for _, endpoint := range []string{"https://evil.example/backend-api/codex/responses", "https://chatgpt.com.evil.example/backend-api/codex/responses", "http://chatgpt.com/backend-api/codex/responses", "https://chatgpt.com/elsewhere", "https://api.openai.com/v1/responses"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if err := svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: cipher}); err == nil {
			t.Fatalf("manual token accepted destination %s", endpoint)
		}
		if req.Header.Get("Authorization") != "" || req.Header.Get("ChatGPT-Account-Id") != "" {
			t.Fatal("rejected destination received auth headers")
		}
	}
	for _, value := range []string{"sk-fixture", plain} {
		if _, err := svc.manualCodexCredentialData(42, value); err == nil {
			t.Fatal("API key/internal envelope manually imported")
		}
	}
}

func TestManualCodexNearExpiryAndExpiredNeverRefresh(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{}`)
	var calls atomic.Int32
	svc.app.HTTP = &http.Client{Transport: codexRoundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("manual access token must never request refresh")
	})}
	// No Redis or database is configured: manual credentials must not need either to refresh.
	credential, err := manualCodexCredential(manualCodexTestToken(time.Now().Add(30 * time.Second).Unix()))
	if err != nil {
		t.Fatal(err)
	}
	current, err := svc.codexCredentialForRequest(context.Background(), credential)
	if err != nil || current != credential {
		t.Fatalf("near-expiry manual credential was refreshed/rejected: %v", err)
	}
	credential.ExpiresAt = time.Now().Unix()
	if _, err := svc.codexCredentialForRequest(context.Background(), credential); err == nil || !strings.Contains(err.Error(), "过期") {
		t.Fatalf("expired manual credential must fail locally: %v", err)
	}
	data, err := svc.manualCodexCredentialData(42, manualCodexTestToken(time.Now().Add(30*time.Second).Unix()))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := secrets.Decrypt(data.ApiKeyCipher.(string))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := decodeCodexCredential(plain)
	if err != nil {
		t.Fatal(err)
	}
	sealed.ExpiresAt = time.Now().Add(-time.Second).Unix()
	expiredCipher, err := secrets.Encrypt(encodeCodexCredential(sealed))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err := svc.ApplyUpstreamAuthHeaders(req, UpstreamAuthInput{Spec: DefaultUpstreamAuthSpec(), CredentialCipher: expiredCipher}); err == nil {
		t.Fatal("expired sealed manual token accepted")
	}
	if calls.Load() != 0 || req.Header.Get("Authorization") != "" {
		t.Fatal("expired manual token caused network request or header injection")
	}
}
