package channel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGeminiProjectSearchScopesAndPagination(t *testing.T) {
	var calls int
	client := &http.Client{Transport: codexRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if r.URL.Scheme != "https" || r.URL.Host != "cloudresourcemanager.googleapis.com" || r.URL.Path != "/v3/projects:search" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer access-fixture" || q.Get("query") != "state:ACTIVE" || q.Get("pageSize") != "25" || q.Get("fields") == "" || r.Body != http.NoBody {
			t.Fatalf("unexpected search request: %s %s", r.Method, r.URL)
		}
		body := `{"projects":[{"projectId":"project-b","displayName":"\u202eB","state":"ACTIVE"},{"projectId":"invalid/id","state":"ACTIVE"},{"projectId":"project-deleted","state":"DELETE_REQUESTED"}],"nextPageToken":"p+1/=&"}`
		if calls == 2 {
			if q.Get("pageToken") != "p+1/=&" {
				t.Fatalf("cursor changed: %v", q)
			}
			body = `{"projects":[{"projectId":"project-a","state":"ACTIVE"},{"projectId":"project-b","state":"ACTIVE"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	tokens := geminiOAuthTokens{AccessToken: "access-fixture", Scope: geminiOAuthScope}
	if projects, message := geminiProjectChoices(context.Background(), client, tokens); calls != 0 || len(projects) != 0 || message != geminiProjectNoScopeMessage {
		t.Fatal("explicit refusal of optional scope must not search")
	}
	tokens.Scope += " " + geminiProjectReadOnlyScope
	projects, message := geminiProjectChoices(context.Background(), client, tokens)
	if calls != 2 || len(projects) != 2 || projects[0].ProjectID != "project-a" || projects[0].DisplayName != "project-a" || projects[1].DisplayName != "B" || message != geminiProjectPickMessage {
		t.Fatalf("wrong normalized project list: calls=%d, projects=%+v, message=%s", calls, projects, message)
	}
}

func TestGeminiProjectSearchFallbackAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		err     error
		message string
	}{
		{name: "denied", status: 403, body: "sensitive error", message: geminiProjectUnavailableMsg},
		{name: "redirect", status: 302, body: "sensitive redirect", message: geminiProjectUnavailableMsg},
		{name: "transport", err: errors.New("sensitive proxy error"), message: geminiProjectUnavailableMsg},
		{name: "oversize", status: 200, body: strings.Repeat(" ", geminiOAuthBodyLimit+1) + `{}`, message: geminiProjectUnavailableMsg},
		{name: "invalid", status: 200, body: `{"projects":`, message: geminiProjectUnavailableMsg},
		{name: "empty", status: 200, body: `{}`, message: geminiProjectEmptyMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			client := &http.Client{Transport: codexRoundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://untrusted.invalid"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			projects, message := geminiProjectChoices(context.Background(), client, geminiOAuthTokens{AccessToken: "access-fixture"})
			if calls != 1 || len(projects) != 0 || message != tc.message {
				t.Fatalf("unsafe or incorrect fallback: calls=%d, message=%s", calls, message)
			}
		})
	}
	var calls int
	client := &http.Client{Transport: codexRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		body := fmt.Sprintf(`{"projects":[{"projectId":"project-%d","state":"ACTIVE"}],"nextPageToken":"cursor-%d"}`, calls, calls)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	projects, message := geminiProjectChoices(context.Background(), client, geminiOAuthTokens{AccessToken: "access-fixture"})
	if calls != geminiProjectSearchMaxPages || len(projects) != calls || message != geminiProjectPartialMessage {
		t.Fatalf("unexpected pagination limit: calls=%d, projects=%d, message=%s", calls, len(projects), message)
	}
}
