package relay

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestRedactSensitiveDataRedactsContentWithoutBreakingToolSchema(t *testing.T) {
	body := []byte(`{
  "model":"gpt-4.1",
  "max_tokens":64,
  "credentials":{"password":"database-password","apiKey":"sk-abc1234567890"},
  "messages":[{"role":"user","content":"Run password=cli-password OPENAI_API_KEY=sk-command-key-123 against https://admin:url-password@example.com with Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.abc.123 for alice@example.com, 13800138000, 11010519491231002X and card 4111 1111 1111 1111."}],
  "tools":[{"type":"function","function":{"name":"connect","parameters":{"type":"object","properties":{"password":{"type":"string"}}}}}]
}`)

	redacted, err := redactSensitiveData(body, redactionSettings())
	if err != nil {
		t.Fatalf("redactSensitiveData() error = %v", err)
	}
	for _, secret := range []string{
		"database-password", "sk-abc1234567890", "cli-password", "sk-command-key-123", "url-password",
		"eyJhbGciOiJIUzI1NiJ9.abc.123", "alice@example.com", "13800138000",
		"11010519491231002X", "4111 1111 1111 1111",
	} {
		if strings.Contains(string(redacted), secret) {
			t.Fatalf("redacted request still contains %q: %s", secret, redacted)
		}
	}
	for _, placeholder := range []string{redactedPassword, redactedToken, redactedPersonal} {
		if !strings.Contains(string(redacted), placeholder) {
			t.Fatalf("redacted request does not contain %q: %s", placeholder, redacted)
		}
	}
	if got := gjson.GetBytes(redacted, "max_tokens").Int(); got != 64 {
		t.Fatalf("max_tokens = %d, want 64", got)
	}
	if got := gjson.GetBytes(redacted, "tools.0.function.parameters.properties.password.type").String(); got != "string" {
		t.Fatalf("tool password schema type = %q, want string", got)
	}
}

func TestRedactSensitiveDataRespectsTotalAndCategorySwitches(t *testing.T) {
	body := []byte(`{"password":"plain-password","token":"sk-abc1234567890","email":"alice@example.com"}`)

	disabled := redactionSettings()
	disabled.SensitiveDataRedactionEnabled = false
	unchanged, err := redactSensitiveData(body, disabled)
	if err != nil {
		t.Fatalf("redactSensitiveData() error = %v", err)
	}
	if string(unchanged) != string(body) {
		t.Fatalf("total switch changed request: %s", unchanged)
	}

	passwordDisabled := redactionSettings()
	passwordDisabled.PasswordRedactionEnabled = false
	redacted, err := redactSensitiveData(body, passwordDisabled)
	if err != nil {
		t.Fatalf("redactSensitiveData() error = %v", err)
	}
	if got := gjson.GetBytes(redacted, "password").String(); got != "plain-password" {
		t.Fatalf("password = %q, want original when category is disabled", got)
	}
	if got := gjson.GetBytes(redacted, "token").String(); got != redactedToken {
		t.Fatalf("token = %q, want %q", got, redactedToken)
	}
	if got := gjson.GetBytes(redacted, "email").String(); got != redactedPersonal {
		t.Fatalf("email = %q, want %q", got, redactedPersonal)
	}
}

func redactionSettings() adminapi.SensitiveWordSettingsInput {
	return adminapi.SensitiveWordSettingsInput{
		SensitiveDataRedactionEnabled: true,
		PasswordRedactionEnabled:      true,
		TokenRedactionEnabled:         true,
		PersonalDataRedactionEnabled:  true,
	}
}
