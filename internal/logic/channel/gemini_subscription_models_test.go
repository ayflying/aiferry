package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type geminiModelsRoundTripper func(*http.Request) (*http.Response, error)

func (rt geminiModelsRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

func TestFetchGeminiSubscriptionModels(t *testing.T) {
	const catalog = `{
		"models": {
			"gemini-3-flash-preview-low": {"displayName":"Gemini Flash (Low)","maxTokens":1048576,"maxOutputTokens":8192,"supportsThinking":true,"supportsImages":true,"quotaInfo":{"remainingFraction":0}},
			"claude-sonnet-4-5": {}, "gpt-oss-120b-medium": {},
			"gemini-disabled": {"disabled":true}, "gemini-internal": {"isInternal":true},
			"gemini-payload-null": null, "gemini-payload-array": [], "gemini bad": {},
			"gemini-2.5-pro": {}, "gemini-2.5-flash-thinking": {},
			"gemini-flash-image": {}, "gemini-embedding-001": {}, "gemini-tts": {},
			"chat_test": {}, "tab_test": {}, "MODEL_DEFAULT": {}, "other-runtime": {}
		}, "defaultAgentModelId":"MODEL_DEFAULT"
	}`
	client := &http.Client{Transport: geminiModelsRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.String() != "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels" {
			t.Fatalf("unexpected method or URL: %s %s", r.Method, r.URL)
		}
		wantHeaders := http.Header{"Accept": {"application/json"}, "Authorization": {"Bearer synthetic-oauth"}, "Content-Type": {"application/json"}}
		if !reflect.DeepEqual(r.Header, wantHeaders) {
			t.Fatalf("unexpected request headers: %v", r.Header)
		}
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !reflect.DeepEqual(body, map[string]any{"project": "project-123"}) {
			t.Fatalf("unexpected model request body: %v, %v", body, err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(catalog))}, nil
	})}
	svc := &sChannel{}
	names, metadata, err := svc.fetchGeminiSubscriptionModels(context.Background(), client, "project-123", "synthetic-oauth")
	if want := []string{"claude-sonnet-4-5", "gemini-3-flash-preview-low", "gpt-oss-120b-medium"}; err != nil || !reflect.DeepEqual(names, want) || len(metadata) != len(want) {
		t.Fatalf("unexpected catalog: %v, %v, %v", names, metadata, err)
	}
	m := metadata["gemini-3-flash-preview-low"]
	if m.DisplayName == nil || *m.DisplayName != "Gemini Flash (Low)" || m.ContextLength == nil || *m.ContextLength != 1048576 || m.MaxOutputTokens == nil || *m.MaxOutputTokens != 8192 || m.Reasoning == nil || !*m.Reasoning {
		t.Fatalf("missing explicit upstream metadata: %+v", m)
	}
	if !reflect.DeepEqual(m.InputModalities, []string{"text"}) || !reflect.DeepEqual(m.OutputModalities, []string{"text"}) || m.Tools == nil || *m.Tools {
		t.Fatalf("metadata must reflect adapter's text-only capabilities: %+v", m)
	}
}

func TestFetchGeminiSubscriptionModelsFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantErr string
		status              int
	}{
		{"empty", `{"models":{}}`, "", http.StatusOK},
		{"missing map", `{}`, "models object", http.StatusOK},
		{"array not a map", `{"models":[]}`, "models object", http.StatusOK},
		{"invalid JSON", `{"models":`, "invalid JSON", http.StatusOK},
		{"authorization rejected", `{"error":{"message":"must-not-leak"}}`, "HTTP 401", http.StatusUnauthorized},
		{"redirect rejected", `must-not-leak`, "HTTP 307", http.StatusTemporaryRedirect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: geminiModelsRoundTripper(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			names, metadata, err := (&sChannel{}).fetchGeminiSubscriptionModels(context.Background(), client, "project-123", "synthetic-oauth")
			if tc.wantErr == "" {
				if err != nil || names == nil || len(names) != 0 || metadata == nil || len(metadata) != 0 {
					t.Fatalf("expected empty catalog, got %v, %v, %v", names, metadata, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) || strings.Contains(err.Error(), "must-not-leak") || names != nil || metadata != nil {
				t.Fatalf("expected sanitized error containing %q, got %v, %v, %v", tc.wantErr, names, metadata, err)
			}
		})
	}
}
