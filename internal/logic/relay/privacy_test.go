package relay

import (
	"net/http"
	"strings"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestRedactGatewayRequestRemovesGatewayDomainBeforeForwarding(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"请访问 https://Gateway.Example:8443/path"}],"metadata":{"origin":"gateway.example"}}`)
	headers := http.Header{
		"User-Agent":       []string{"client at gateway.example"},
		"Origin":           []string{"https://gateway.example"},
		"Referer":          []string{"https://gateway.example/settings"},
		"X-Forwarded-Host": []string{"gateway.example"},
	}

	redactedBody, redactedHeaders := redactGatewayRequest(body, headers, "gateway.example:8443", adminapi.SensitiveWordSettingsInput{SensitiveDataRedactionEnabled: true})
	if strings.Contains(strings.ToLower(string(redactedBody)), "gateway.example") {
		t.Fatalf("gateway host remained in body: %s", redactedBody)
	}
	if !strings.Contains(string(redactedBody), redactedGatewayHost) {
		t.Fatalf("gateway host was not redacted: %s", redactedBody)
	}
	if strings.Contains(strings.ToLower(redactedHeaders.Get("User-Agent")), "gateway.example") {
		t.Fatalf("gateway host remained in forwarded user agent: %q", redactedHeaders.Get("User-Agent"))
	}
	for _, name := range []string{"Origin", "Referer", "X-Forwarded-Host"} {
		if value := redactedHeaders.Get(name); value != "" {
			t.Fatalf("%s must not be forwarded: %q", name, value)
		}
	}
	if headers.Get("Origin") == "" {
		t.Fatal("input headers were mutated")
	}
	forwardedHeaders := http.Header{}
	copyRequestHeaders(forwardedHeaders, redactedHeaders)
	if strings.Contains(strings.ToLower(forwardedHeaders.Get("User-Agent")), "gateway.example") {
		t.Fatalf("gateway host reached upstream user agent: %q", forwardedHeaders.Get("User-Agent"))
	}
	for _, name := range []string{"Origin", "Referer", "X-Forwarded-Host"} {
		if value := forwardedHeaders.Get(name); value != "" {
			t.Fatalf("%s reached upstream: %q", name, value)
		}
	}
}

func TestRedactGatewayRequestLeavesUnrelatedTextUntouched(t *testing.T) {
	body := []byte(`{"input":"https://other.example/path"}`)
	redacted, _ := redactGatewayRequest(body, http.Header{}, "gateway.example", adminapi.SensitiveWordSettingsInput{SensitiveDataRedactionEnabled: true})
	if string(redacted) != string(body) {
		t.Fatalf("unrelated content changed: %s", redacted)
	}
}

func TestRedactGatewayRequestRespectsDisabledDataRedaction(t *testing.T) {
	body := []byte(`{"input":"https://gateway.example/path"}`)
	headers := http.Header{"User-Agent": []string{"client at gateway.example"}, "Origin": []string{"https://gateway.example"}}

	forwardedBody, forwardedHeaders := redactGatewayRequest(body, headers, "gateway.example", adminapi.SensitiveWordSettingsInput{SensitiveDataRedactionEnabled: false})
	if string(forwardedBody) != string(body) {
		t.Fatalf("gateway host was redacted while disabled: %s", forwardedBody)
	}
	if forwardedHeaders.Get("User-Agent") != headers.Get("User-Agent") || forwardedHeaders.Get("Origin") != headers.Get("Origin") {
		t.Fatalf("headers changed while disabled: %#v", forwardedHeaders)
	}
	if forwardedHeaders.Get("User-Agent") == "" {
		t.Fatal("forwarded headers must not alias the input headers")
	}
}
