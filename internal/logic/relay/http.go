package relay

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yunloli/aiferry/internal/logic/upstreamerror"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

func (s *sRelay) writeBufferedResponse(writer http.ResponseWriter, status int, body []byte, headers http.Header) {
	copyResponseHeaders(writer.Header(), headers)
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func copyRequestHeaders(target, source http.Header) {
	for _, name := range []string{"Accept", "Content-Type", "User-Agent", "OpenAI-Beta", "Idempotency-Key", "Range"} {
		for _, value := range source.Values(name) {
			target.Add(name, value)
		}
	}
}

func copyResponseHeaders(target, source http.Header) {
	for name, values := range source {
		if dropClientResponseHeader(name) {
			continue
		}
		for _, value := range values {
			target.Add(name, value)
		}
	}
}

// dropClientResponseHeader 判断上游响应头是否可以回写给客户端。
//
//   - 逐跳头属于本次上游连接，跨连接转发没有意义；
//   - Content-Length 必须重新计算：net/http 读取上游体时按 Content-Encoding 自动解压
//     并删除了上游的这个头（以及 Content-Encoding），剩下的值往往已经与明文长度不符；
//   - Content-Encoding / Transfer-Encoding 同理：网关写回的是解压后的明文，若原样
//     复制，客户端会按错误声明去解析；压缩由网关全局响应压缩钩子重新决定。
//
// 该函数同样用于视频等直写缓冲的端点，避免它们带着错误长度头再被网关压一次。
func dropClientResponseHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "content-length", "content-encoding":
		return true
	default:
		return false
	}
}

// DropClientResponseHeader 是 dropClientResponseHeader 的导出包装，供控制器层
// 把上游响应头回写给客户端时复用同一套规则（视频/音频等直写端点）。
func DropClientResponseHeader(name string) bool {
	return dropClientResponseHeader(name)
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
	return OpenAIErrorResponse(kind, message)
}

// OpenAIErrorResponse creates the shared OpenAI-compatible error payload for controller fallbacks.
func OpenAIErrorResponse(kind, message string) []byte {
	payload, _ := json.Marshal(map[string]any{"error": map[string]any{"type": kind, "message": message}})
	return payload
}

func upstreamError(body []byte, fallback string) string {
	return upstreamerror.Message(body, fallback)
}

func newRequestID() string {
	return usage.NewRequestID("afreq")
}
