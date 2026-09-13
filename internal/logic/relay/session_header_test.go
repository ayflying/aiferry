package relay

import (
	"net/http"
	"testing"
)

func TestApplyUpstreamSessionHeaderInjectsStableSessionForOpenCodeGo(t *testing.T) {
	candidate := Candidate{ChannelType: openCodeGoChannelType, ChannelID: 7, ChannelCredentialID: 11, PublicName: "grok-4.6"}

	first := http.Header{}
	applyUpstreamSessionHeader(first, http.Header{}, candidate, 42)
	second := http.Header{}
	applyUpstreamSessionHeader(second, nil, candidate, 42)

	session := first.Get(openCodeSessionHeader)
	if session == "" {
		t.Fatal("expected x-opencode-session to be set for opencode_go")
	}
	if second.Get(openCodeSessionHeader) != session {
		t.Fatalf("expected stable session, got %q and %q", session, second.Get(openCodeSessionHeader))
	}
	other := http.Header{}
	applyUpstreamSessionHeader(other, http.Header{}, candidate, 43)
	if other.Get(openCodeSessionHeader) == session {
		t.Fatal("expected different users to derive different sessions")
	}
}

func TestApplyUpstreamSessionHeaderPassesThroughClientSession(t *testing.T) {
	candidate := Candidate{ChannelType: openCodeGoChannelType, ChannelID: 7, PublicName: "kimi-k3"}
	for _, name := range []string{"x-opencode-session", "session_id", "X-Session-Id", "conversation_id"} {
		incoming := http.Header{}
		incoming.Set(name, "client-session-1")
		target := http.Header{}
		applyUpstreamSessionHeader(target, incoming, candidate, 1)
		if target.Get(openCodeSessionHeader) != "client-session-1" {
			t.Fatalf("expected %s to be forwarded, got %q", name, target.Get(openCodeSessionHeader))
		}
	}
}

func TestApplyUpstreamSessionHeaderSkipsOtherChannelTypes(t *testing.T) {
	target := http.Header{}
	applyUpstreamSessionHeader(target, http.Header{}, Candidate{ChannelType: "openai"}, 1)
	if target.Get(openCodeSessionHeader) != "" {
		t.Fatal("expected no session header for non-opencode channels")
	}
}
