package relay

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yunloli/aiferry/internal/service/upstreamerror"
	"github.com/yunloli/aiferry/internal/service/usage"
)

func (s *Service) writeBufferedResponse(writer http.ResponseWriter, status int, body []byte, headers http.Header) {
	copyResponseHeaders(writer.Header(), headers)
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func copyRequestHeaders(target, source http.Header) {
	for _, name := range []string{"Accept", "User-Agent", "OpenAI-Beta", "Idempotency-Key"} {
		for _, value := range source.Values(name) {
			target.Add(name, value)
		}
	}
}

func copyResponseHeaders(target, source http.Header) {
	for name, values := range source {
		if hopByHopHeader(name) || strings.EqualFold(name, "Content-Length") {
			continue
		}
		for _, value := range values {
			target.Add(name, value)
		}
	}
}

func hopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func openAIError(kind, message string) []byte {
	payload, _ := json.Marshal(map[string]any{"error": map[string]any{"type": kind, "message": message}})
	return payload
}

func upstreamError(body []byte, fallback string) string {
	return upstreamerror.Message(body, fallback)
}

func newRequestID() string {
	return usage.NewRequestID("afreq")
}
