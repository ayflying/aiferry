package relay

import (
	"encoding/json"
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

	redacted, restorer, err := redactSensitiveDataWithRestore(body, redactionSettings())
	if err != nil {
		t.Fatalf("redactSensitiveDataWithRestore() error = %v", err)
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
	if !strings.Contains(string(redacted), "aiferry_ref_") {
		t.Fatalf("redacted request does not contain a request-scoped placeholder: %s", redacted)
	}
	restored := restorer.restoreBufferedResponse(redacted)
	for _, secret := range []string{
		"database-password", "sk-abc1234567890", "cli-password", "sk-command-key-123", "url-password",
		"eyJhbGciOiJIUzI1NiJ9.abc.123", "alice@example.com", "13800138000",
		"11010519491231002X", "4111 1111 1111 1111",
	} {
		if !strings.Contains(string(restored), secret) {
			t.Fatalf("restored request does not contain %q: %s", secret, restored)
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
	if got := gjson.GetBytes(redacted, "token").String(); !strings.HasPrefix(got, "aiferry_ref_") {
		t.Fatalf("token = %q, want a request-scoped placeholder", got)
	}
	if got := gjson.GetBytes(redacted, "email").String(); !strings.HasPrefix(got, "aiferry_ref_") {
		t.Fatalf("email = %q, want a request-scoped placeholder", got)
	}
}

func TestRedactSensitiveDataKeepsResponsesShapeAndUsesWireSafePlaceholders(t *testing.T) {
	body := []byte(`{
  "model":"gpt-4.1",
  "stream":true,
  "input":[{"role":"user","content":[{"type":"input_text","text":"Deploy with password=demo-password and token=sk-demo-token-123456789."}]}],
  "tools":[{"type":"function","name":"deploy","parameters":{"type":"object","properties":{"password":{"type":"string"}}}}]
}`)

	redacted, restorer, err := redactSensitiveDataWithRestore(body, redactionSettings())
	if err != nil {
		t.Fatalf("redactSensitiveDataWithRestore() error = %v", err)
	}
	if !gjson.GetBytes(redacted, "stream").Bool() {
		t.Fatalf("stream flag changed: %s", redacted)
	}
	if got := gjson.GetBytes(redacted, "input.0.content.0.type").String(); got != "input_text" {
		t.Fatalf("Responses input type = %q, want input_text", got)
	}
	if got := gjson.GetBytes(redacted, "tools.0.parameters.properties.password.type").String(); got != "string" {
		t.Fatalf("Responses tool schema type = %q, want string", got)
	}
	if input := gjson.GetBytes(redacted, "input.0.content.0.text").String(); strings.Contains(input, "demo-password") || strings.Contains(input, "sk-demo-token-123456789") {
		t.Fatalf("Responses input still contains a sensitive value: %s", input)
	}
	if len(restorer.replacements) == 0 {
		t.Fatal("expected request-scoped replacements")
	}
	for placeholder := range restorer.replacements {
		if !strings.HasPrefix(placeholder, "aiferry_ref_") || strings.ContainsAny(placeholder, "[]:") || strings.Contains(strings.ToLower(placeholder), "secret") {
			t.Fatalf("placeholder is not safe for an upstream text request: %q", placeholder)
		}
		if !strings.Contains(string(redacted), placeholder) {
			t.Fatalf("outbound Responses request is missing placeholder %q", placeholder)
		}
	}
}

func TestRedactSensitiveDataLeavesSafeResponsesRequestUntouched(t *testing.T) {
	body := []byte(`{
  "model":"gpt-4.1",
  "stream":true,
  "max_output_tokens":64,
  "input":[{"role":"user","content":[{"type":"input_text","text":"Summarize this release note."}]}]
}`)

	redacted, restorer, err := redactSensitiveDataWithRestore(body, redactionSettings())
	if err != nil {
		t.Fatalf("redactSensitiveDataWithRestore() error = %v", err)
	}
	if string(redacted) != string(body) {
		t.Fatalf("safe Responses request was rewritten:\n%s", redacted)
	}
	if restorer != nil {
		t.Fatal("safe Responses request unexpectedly created a restorer")
	}
}

func TestSensitiveDataRestorerRestoresOnlyCurrentRequestPlaceholders(t *testing.T) {
	request := []byte(`{"messages":[{"role":"user","content":"token=sk-abc1234567890"}]}`)
	_, restorer, err := redactSensitiveDataWithRestore(request, redactionSettings())
	if err != nil {
		t.Fatalf("redactSensitiveDataWithRestore() error = %v", err)
	}
	var placeholder string
	for value := range restorer.replacements {
		placeholder = value
	}
	response := sensitiveJSONForTest(t, map[string]any{
		"choices":    []any{map[string]any{"message": map[string]any{"content": "Use " + placeholder}}},
		"tool_calls": []any{map[string]any{"function": map[string]any{"arguments": `{"token":"` + placeholder + `"}`}}},
	})
	restored := restorer.restoreBufferedResponse(response)
	if strings.Contains(string(restored), placeholder) {
		t.Fatalf("restored response still contains placeholder: %s", restored)
	}
	if got := gjson.GetBytes(restored, "choices.0.message.content").String(); got != "Use sk-abc1234567890" {
		t.Fatalf("message content = %q", got)
	}
	if got := gjson.GetBytes(restored, "tool_calls.0.function.arguments").String(); got != `{"token":"sk-abc1234567890"}` {
		t.Fatalf("tool arguments = %q", got)
	}
	other := newSensitiveDataRestorerForTest(t)
	if got := other.restoreBufferedResponse(response); strings.Contains(string(got), "sk-abc1234567890") {
		t.Fatal("a different request restored a placeholder it did not create")
	}
}

func TestSensitiveDataStreamRestorerHandlesSplitPlaceholder(t *testing.T) {
	request := []byte(`{"messages":[{"role":"user","content":"token=sk-abc1234567890"}]}`)
	_, restorer, err := redactSensitiveDataWithRestore(request, redactionSettings())
	if err != nil {
		t.Fatalf("redactSensitiveDataWithRestore() error = %v", err)
	}
	var placeholder string
	for value := range restorer.replacements {
		placeholder = value
	}
	streamRestorer := newSensitiveDataStreamRestorer(restorer)
	first := streamRestorer.restoreSSELine(sensitiveSSELineForTest(t, "Use "+placeholder[:16]))
	if len(first) != 1 {
		t.Fatalf("first response lines = %d, want 1", len(first))
	}
	firstPayload, _, valid := sseDataPayload(first[0])
	if !valid {
		t.Fatalf("first response is not a valid SSE payload: %s", first[0])
	}
	if got := gjson.GetBytes(firstPayload, "choices.0.delta.content").String(); got != "Use " {
		t.Fatalf("first chunk = %q, want %q", got, "Use ")
	}
	second := streamRestorer.restoreSSELine(sensitiveSSELineForTest(t, placeholder[16:]+" now"))
	if len(second) != 1 {
		t.Fatalf("second response lines = %d, want 1", len(second))
	}
	secondPayload, _, valid := sseDataPayload(second[0])
	if !valid {
		t.Fatalf("second response is not a valid SSE payload: %s", second[0])
	}
	if got := gjson.GetBytes(secondPayload, "choices.0.delta.content").String(); got != "sk-abc1234567890 now" {
		t.Fatalf("second chunk = %q", got)
	}
}

func newSensitiveDataRestorerForTest(t *testing.T) *sensitiveDataRestorer {
	t.Helper()
	restorer, err := newSensitiveDataRestorer()
	if err != nil {
		t.Fatalf("newSensitiveDataRestorer() error = %v", err)
	}
	return restorer
}

func sensitiveJSONForTest(t *testing.T, payload any) []byte {
	t.Helper()
	result, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return result
}

func sensitiveSSELineForTest(t *testing.T, content string) []byte {
	t.Helper()
	payload := sensitiveJSONForTest(t, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": content}}},
	})
	return append(append([]byte("data: "), payload...), '\n')
}

func redactionSettings() adminapi.SensitiveWordSettingsInput {
	return adminapi.SensitiveWordSettingsInput{
		SensitiveDataRedactionEnabled: true,
		PasswordRedactionEnabled:      true,
		TokenRedactionEnabled:         true,
		PersonalDataRedactionEnabled:  true,
	}
}
