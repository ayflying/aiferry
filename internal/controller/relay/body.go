package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// 各端点的请求体上限（字节）。控制器层会先把整个请求体同步缓冲进内存再交给
// relay 层，因此上限只能在这里声明：io.LimitReader 只会静默截断，
// 截断后的内容不是合法 JSON，会把「请求体过大」伪装成难以定位的解析错误。
//
// 这些值必须小于 GoFrame 的 server.clientMaxBodySize（config.yaml 中配置为 96MiB）。
// GoFrame 默认上限是 8MiB，一旦小于这里就会在控制器读取前先拦掉请求。
const (
	maxChatRequestBodyLimit   = 32 << 20
	maxAudioRequestBodyLimit  = 24 << 20
	maxImagesRequestBodyLimit = 32 << 20
	maxVideoRequestBodyLimit  = 64 << 20
)

// errBodyLimitExceeded 用于超限时的日志，便于按日志检索这一类拒绝。
var errBodyLimitExceeded = errors.New("request body exceeds the configured limit")

// bodyReadFailure 描述一次请求体读取失败应如何回写客户端。
type bodyReadFailure struct {
	status  int
	kind    string
	message string
}

// readRequestBody 读取并校验请求体，失败时已写好错误响应并返回 false。
//
// 控制器层的拒绝发生在 relay 之前，既不落 usage_logs 也不落原始报文，
// 所以每次失败都必须留下日志，否则线上「对话直接中断」将完全无法定位。
func readRequestBody(r *ghttp.Request, limit int64) ([]byte, bool) {
	body, exceeded, err := readLimitedBody(r.Body, limit)
	switch {
	case err != nil:
		failure := classifyBodyReadFailure(err, limit)
		logBodyReadFailure(r, limit, len(body), err)
		writeError(r, failure.status, failure.kind, failure.message)
		return nil, false
	case exceeded:
		failure := bodyLimitFailure(limit)
		logBodyReadFailure(r, limit, len(body), errBodyLimitExceeded)
		writeError(r, failure.status, failure.kind, failure.message)
		return nil, false
	}
	return body, true
}

// readLimitedBody 读取至多 limit 字节。超出上限时多读 1 字节即可判定，
// 避免把整个超大请求体读进内存；返回的 body 仍是完整读到的内容。
func readLimitedBody(reader io.Reader, limit int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return body, false, err
	}
	return body, int64(len(body)) > limit, nil
}

// classifyBodyReadFailure 把底层读取错误映射为对客户端有意义的响应。
//
//   - 超过网关上限（GoFrame 的 MaxBytesReader）→ 413，并给出真实上限，
//     让调用方知道该缩短上下文或附件，而不是面对一句无从下手的读取失败。
//   - 传输中断（客户端中途断开/上传超时）→ 400 并注明可重试。
//   - 其余未知错误 → 保留原有文案，避免改变既有兼容行为。
func classifyBodyReadFailure(err error, limit int64) bodyReadFailure {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return bodyLimitFailure(maxBytes.Limit)
	}
	if isClientAbortedBodyRead(err) {
		return bodyReadFailure{
			status:  http.StatusBadRequest,
			kind:    "invalid_request_error",
			message: "Request body was not received completely because the connection closed mid-upload. Please retry the request.",
		}
	}
	return bodyReadFailure{
		status:  http.StatusBadRequest,
		kind:    "invalid_request_error",
		message: "Unable to read request body",
	}
}

// bodyLimitFailure 返回请求体超限的响应，limit 为真实生效的字节上限。
func bodyLimitFailure(limit int64) bodyReadFailure {
	return bodyReadFailure{
		status: http.StatusRequestEntityTooLarge,
		kind:   "invalid_request_error",
		message: "Request body exceeds the " + humanByteSize(limit) +
			" limit. Shorten the conversation or attachments and retry.",
	}
}

// isClientAbortedBodyRead 判断错误是否来自客户端提前断开导致的读取中断，
// 而非网关自身的限制或内部故障。
func isClientAbortedBodyRead(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, token := range []string{
		"unexpected eof",
		"connection reset",
		"broken pipe",
		"use of closed network connection",
		"client disconnected",
		"econnreset",
		"epipe",
	} {
		if strings.Contains(message, token) {
			return true
		}
	}
	return false
}

// humanByteSize 把字节上限格式化为 "32 MiB" 这样的可读文本。
func humanByteSize(limit int64) string {
	if limit <= 0 {
		return "configured"
	}
	if limit%(1<<20) == 0 {
		return strconv.FormatInt(limit>>20, 10) + " MiB"
	}
	return strconv.FormatInt(limit, 10) + " bytes"
}

// logBodyReadFailure 记录请求体读取失败。控制器层的拒绝不落库、不落报文，
// 这条 WARN 日志是线上定位该类中断的唯一线索。
func logBodyReadFailure(r *ghttp.Request, limit int64, readBytes int, err error) {
	g.Log().Warningf(r.Context(),
		"read request body failed: endpoint=%s client=%s contentType=%q contentLength=%d readBytes=%d limit=%d error=%v",
		r.URL.Path, clientIP(r), r.Header.Get("Content-Type"), r.ContentLength, readBytes, limit, err)
}
