package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/protocol"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

// 真实 HTTP 解析与重试策略的无 DB 集成边界；不模拟选号、计分或退避等待。
func TestTransientRetryHTTPUncommittedOverload(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{"HTTP502JSON", http.StatusBadGateway, "application/json", `{"error":{"message":"upstream overloaded","type":"server_error"}}`},
		{"HTTP200SSEError", http.StatusOK, "text/event-stream", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"error\":{\"message\":\"upstream overloaded\",\"type\":\"server_error\"}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			relay, candidate := transientHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			writer := httptest.NewRecorder()
			result, _, err := relay.attempt(context.Background(), writer, http.Header{}, protocol.ChatCompletionsEndpoint, []byte(`{"model":"qwen-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`), candidate, true, 11, 42, transientHTTPSettings(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("upstream calls = %d, want 1", calls.Load())
			}
			if result.status != http.StatusBadGateway || !strings.Contains(result.errorMessage, "overloaded") {
				t.Fatalf("unexpected parsed overload: status=%d message=%q", result.status, result.errorMessage)
			}
			if result.wroteBytes || result.writerFailed || result.streamCompleted || writer.Body.Len() != 0 {
				t.Fatalf("overload must not commit output: wrote=%t failed=%t completed=%t body=%q", result.wroteBytes, result.writerFailed, result.streamCompleted, writer.Body.String())
			}
			if attemptCompleted(result, err, true) {
				t.Fatal("uncommitted overload must stay in retry chain")
			}
			last := channelAttempt{candidate: candidate, result: result}
			if delay, ok := sameCandidateRetryDelay(attemptOptions{transientRetry: true}, 0, last, nil); !ok || delay != transientRetryDelays[0] {
				t.Fatalf("retry = (%v, %t), want first transient delay", delay, ok)
			}
		})
	}
}

func TestTransientRetryHTTPFirstWriteFailureCannotReplay(t *testing.T) {
	relay, candidate := transientHTTPFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	writer := &transientFailingWriter{header: make(http.Header)}
	result, _, err := relay.attempt(context.Background(), writer, http.Header{}, protocol.ChatCompletionsEndpoint, []byte(`{"model":"qwen-test","stream":true,"messages":[{"role":"user","content":"hi"}]}`), candidate, true, 11, 42, transientHTTPSettings(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if writer.status != http.StatusOK || writer.writes != 1 || !result.writerFailed || result.wroteBytes {
		t.Fatalf("first write boundary: status=%d writes=%d failed=%t wrote=%t", writer.status, writer.writes, result.writerFailed, result.wroteBytes)
	}
	if !attemptCompleted(result, nil, true) {
		t.Fatal("downstream first-write failure must end the attempt")
	}
	// 即使调用者随后把错误规范化成 502，也不得重新向断开的客户端重放。
	result = failedAttemptResult(result, "downstream write failed")
	if _, ok := sameCandidateRetryDelay(attemptOptions{transientRetry: true}, 0, channelAttempt{candidate: candidate, result: result}, nil); ok {
		t.Fatal("first-write failure must not receive a transient retry")
	}
}

func transientHTTPFixture(t *testing.T, handler http.Handler) (*sRelay, Candidate) {
	t.Helper()
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	appSvc := &app.Service{Secrets: secrets, HTTP: upstream.Client()}
	return &sRelay{
		app:      appSvc,
		channels: channel.New(appSvc, channeltype.New(&config.BuiltinRegistry{}, nil), nil, nil, nil, nil, nil, nil),
		prices:   pricingcache.New(),
	}, Candidate{ChannelID: 7, ChannelCredentialID: 22, ChannelName: "transient-http-test", ChannelType: "probe_fallback", BaseURL: upstream.URL, UpstreamName: "qwen-test"}
}

func transientHTTPSettings() adminapi.SystemResilienceSettingsInput {
	return adminapi.SystemResilienceSettingsInput{StreamFirstByteTimeoutSeconds: 5, StreamIdleTimeoutSeconds: 5}
}

type transientFailingWriter struct {
	header http.Header
	status int
	writes int
}

func (w *transientFailingWriter) Header() http.Header    { return w.header }
func (w *transientFailingWriter) WriteHeader(status int) { w.status = status }
func (w *transientFailingWriter) Write(_ []byte) (int, error) {
	w.writes++
	return 0, errors.New("client disconnected")
}
