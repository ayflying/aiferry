package relay

import (
	"net/http"
	"testing"
)

func TestApplyOpencodeGoHeadersUsesCandidateIdentity(t *testing.T) {
	candidate := Candidate{ChannelType: "opencode_go", ChannelID: 7, ChannelCredentialID: 11, PublicName: "kimi-k3"}
	target := http.Header{}
	applyOpencodeGoHeaders(target, http.Header{}, candidate, 42)
	if target.Get("x-opencode-session") == "" {
		t.Fatal("expected session header for opencode_go candidates")
	}

	other := http.Header{}
	applyOpencodeGoHeaders(other, http.Header{}, Candidate{ChannelType: "openai"}, 42)
	if other.Get("x-opencode-session") != "" {
		t.Fatal("expected no session header for other channel types")
	}
}
