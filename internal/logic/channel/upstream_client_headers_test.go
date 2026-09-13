package channel

import (
	"net/http"
	"strings"
	"testing"
)

func openCodeIdentity() UpstreamClientIdentity {
	return UpstreamClientIdentity{ChannelType: OpenCodeGoChannelType, ChannelID: 7, CredentialID: 11, ModelName: "kimi-k3"}
}

func TestApplyUpstreamClientHeadersInjectsStableSession(t *testing.T) {
	first := http.Header{}
	ApplyUpstreamClientHeaders(first, http.Header{}, openCodeIdentity())
	second := http.Header{}
	ApplyUpstreamClientHeaders(second, nil, openCodeIdentity())

	session := first.Get(openCodeSessionHeader)
	if session == "" {
		t.Fatal("expected x-opencode-session to be set for opencode_go")
	}
	if second.Get(openCodeSessionHeader) != session {
		t.Fatalf("expected stable session, got %q and %q", session, second.Get(openCodeSessionHeader))
	}
	if !strings.HasPrefix(session, "aiferry-") {
		t.Fatalf("expected derived session to be namespaced, got %q", session)
	}

	other := openCodeIdentity()
	other.CredentialID = 12
	third := http.Header{}
	ApplyUpstreamClientHeaders(third, http.Header{}, other)
	if third.Get(openCodeSessionHeader) == session {
		t.Fatal("expected different credentials to derive different sessions")
	}
}

func TestApplyUpstreamClientHeadersPassesThroughClientSession(t *testing.T) {
	for _, name := range []string{"x-opencode-session", "session_id", "X-Session-Id", "conversation_id"} {
		incoming := http.Header{}
		incoming.Set(name, "client-session-1")
		target := http.Header{}
		ApplyUpstreamClientHeaders(target, incoming, openCodeIdentity())
		if target.Get(openCodeSessionHeader) != "client-session-1" {
			t.Fatalf("expected %s to be forwarded, got %q", name, target.Get(openCodeSessionHeader))
		}
	}
}

func TestApplyUpstreamClientHeadersKeepsAgentUserAgent(t *testing.T) {
	for _, agent := range []string{"codex_cli_rs/0.20.0", "claude-cli/1.0.60 (external, cli)", "opencode/0.5.1", "my-coding-agent/1.0"} {
		incoming := http.Header{}
		incoming.Set("User-Agent", agent)
		target := http.Header{}
		ApplyUpstreamClientHeaders(target, incoming, openCodeIdentity())
		if got := target.Get("User-Agent"); got != agent {
			t.Fatalf("expected agent UA %q to be preserved, got %q", agent, got)
		}
	}
}

func TestApplyUpstreamClientHeadersReplacesGenericUserAgent(t *testing.T) {
	for _, agent := range []string{"OpenAI/Python 1.55.3", "openai-node/4.0.0", "Go-http-client/2.0", "python-requests/2.31.0", "curl/8.6.0", "Mozilla/5.0", ""} {
		incoming := http.Header{}
		if agent != "" {
			incoming.Set("User-Agent", agent)
		}
		target := http.Header{}
		ApplyUpstreamClientHeaders(target, incoming, openCodeIdentity())
		if got := target.Get("User-Agent"); !strings.HasPrefix(got, gatewayUserAgentPrefix) {
			t.Fatalf("expected generic UA %q to be replaced, got %q", agent, got)
		}
	}
}

func TestApplyUpstreamClientHeadersSkipsOtherChannelTypes(t *testing.T) {
	incoming := http.Header{}
	incoming.Set("User-Agent", "Go-http-client/2.0")
	target := http.Header{}
	identity := openCodeIdentity()
	identity.ChannelType = "openai"
	ApplyUpstreamClientHeaders(target, incoming, identity)
	if target.Get(openCodeSessionHeader) != "" {
		t.Fatal("expected no session header for non-opencode channels")
	}
	if target.Get("User-Agent") != "" {
		t.Fatal("expected other channel types to keep their own UA handling")
	}
}
