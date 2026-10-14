package channel

import (
	"net/http"
	"testing"

	"github.com/yunloli/aiferry/internal/buildinfo"
)

func TestWorkBuddyNormalizesLibraryUserAgent(t *testing.T) {
	for _, ua := range []string{"", "python-requests/2.32.3", "Python-Requests/2.31.0", "Python-urllib/3.12", "Go-http-client/1.1", "OpenAI/Python 1.0", "curl/8.6.0"} {
		t.Run(ua, func(t *testing.T) {
			incoming := http.Header{}
			incoming.Set("User-Agent", ua)
			incoming.Set("X-Session-Id", "client-session")
			target := incoming.Clone()
			ApplyUpstreamClientHeaders(target, incoming, UpstreamClientIdentity{ChannelType: "workbuddy"})
			if got, want := target.Get("User-Agent"), gatewayUserAgentPrefix+buildinfo.RuntimeVersion(); got != want {
				t.Fatalf("User-Agent = %q, want %q", got, want)
			}
			if incoming.Get("User-Agent") != ua || target.Get("X-Session-Id") != "client-session" {
				t.Fatal("mutated incoming headers or session")
			}
			if target.Get(openCodeSessionHeader) != "" {
				t.Fatal("WorkBuddy must not receive an injected OpenCode session")
			}
		})
	}
}

func TestWorkBuddyPreservesNamedAgentUserAgent(t *testing.T) {
	for _, ua := range []string{"DeepSeekHarness/1.0", "codex_cli_rs/0.1", "aiferry/0.6.5"} {
		incoming := http.Header{"User-Agent": []string{ua}}
		target := incoming.Clone()
		ApplyUpstreamClientHeaders(target, incoming, UpstreamClientIdentity{ChannelType: "workbuddy"})
		if target.Get("User-Agent") != ua {
			t.Fatalf("named client %q was replaced", ua)
		}
	}
}
