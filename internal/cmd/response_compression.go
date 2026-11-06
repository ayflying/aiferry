package cmd

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gogf/gf/v2/net/ghttp"
)

// 响应压缩的取用阈值（字节）。小于该长度的响应压缩后往往更大，
// 而且客户端多为一次性小包，压了只是白烧 CPU。
const responseCompressionMinBytes = 1024

var (
	gzipWriterPool = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}
	// deflate 用原始 DEFLATE 流（RFC 1951），与浏览器对 "deflate" 的常规实现一致。
	deflateWriterPool = sync.Pool{New: func() any {
		writer, err := flate.NewWriter(nil, flate.DefaultCompression)
		if err != nil {
			// 固定压缩级别下不会失败；真失败说明参数已改坏，属编码错误。
			panic(err)
		}
		return writer
	}}
)

// acceptEncodingOf 读取客户端的 Accept-Encoding；请求对象缺失时按不支持压缩处理。
func acceptEncodingOf(r *ghttp.Request) string {
	if r == nil || r.Request == nil {
		return ""
	}
	return r.Request.Header.Get("Accept-Encoding")
}

// NegotiateResponseCompression 是全局响应压缩，挂在 HookBeforeOutput 上。
//
// 放在输出前钩子而不是路由中间件的原因：
//   - GoFrame 的静态文件检索优先于动态路由，前端分包与 index.html 不经过任何路由
//     中间件，只有输出前钩子能覆盖全部响应；
//   - 转发接口有的直接写 RawWriter（流式早退、视频/音频缓冲），路由中间件包装的
//     Writer 未必被采用；钩子直接改最终缓冲区，无论哪种写法都生效。
//
// 不会重复压缩：GoFrame 内置的 ghttp.MiddlewareGzip 会在它的 AfterHandler 阶段
// 就把缓冲区压好并写上 Content-Encoding；集群内部接口也是「自己压好 + 设置头」。
// 本钩子看到已有 Content-Encoding 就跳过，因此天然兼容这两条既有压缩路径。
func NegotiateResponseCompression(r *ghttp.Request) {
	if r == nil || r.Response == nil {
		return
	}
	response := r.Response
	if response.IsHijacked() {
		return
	}
	header := response.Header()
	// 已经压缩过：保持原样，不再压第二层。
	if header.Get("Content-Encoding") != "" {
		return
	}
	// 无响应体、或语义上不允许压缩的状态码。
	// 注意不能用 r.Method / r.URL：状态异常处理器等内部请求可能只挂了响应对象。
	if response.Status == http.StatusNoContent || response.Status == http.StatusNotModified ||
		(r.Request != nil && r.Request.Method == http.MethodHead && response.BufferLength() == 0) {
		return
	}
	body := response.Buffer()
	if len(body) < responseCompressionMinBytes {
		return
	}
	encoding := negotiatedCompressionEncoding(acceptEncodingOf(r))
	if encoding == "" {
		return
	}
	compressed, err := compressResponseBody(body, encoding)
	if err != nil || len(compressed) >= len(body) {
		// 压不动（已是压缩格式的二进制、内容过于随机）就原样输出：
		// 宁可不压，也不能发出比明文更大的响应。
		return
	}
	response.ClearBuffer()
	header.Set("Content-Encoding", encoding)
	// 长度必须按压缩后的字节重新给出，并声明 Vary 让缓存按编码区分。
	header.Del("Content-Length")
	header.Set("Content-Length", strconv.Itoa(len(compressed)))
	addVaryAcceptEncoding(header)
	response.Write(compressed)
}

// compressResponseBody 按协商出的编码压缩响应体。
func compressResponseBody(body []byte, encoding string) ([]byte, error) {
	var (
		buffer bytes.Buffer
		err    error
	)
	switch encoding {
	case "gzip":
		writer := gzipWriterPool.Get().(*gzip.Writer)
		writer.Reset(&buffer)
		if _, err = writer.Write(body); err == nil {
			err = writer.Close()
		}
		writer.Reset(nil)
		gzipWriterPool.Put(writer)
	case "deflate":
		writer := deflateWriterPool.Get().(*flate.Writer)
		writer.Reset(&buffer)
		if _, err = writer.Write(body); err == nil {
			err = writer.Close()
		}
		writer.Reset(nil)
		deflateWriterPool.Put(writer)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// negotiatedCompressionEncoding 按 Accept-Encoding 的 q 值选择编码：
// gzip 优先（客户端与中间代理支持最广），其次 deflate；
// 都不可接受时返回空串，表示不压缩。
func negotiatedCompressionEncoding(acceptEncoding string) string {
	acceptEncoding = strings.TrimSpace(acceptEncoding)
	if acceptEncoding == "" {
		return ""
	}
	gzipQuality, gzipOK := acceptEncodingQuality(acceptEncoding, "gzip")
	deflateQuality, deflateOK := acceptEncodingQuality(acceptEncoding, "deflate")
	switch {
	case gzipOK && gzipQuality > 0 && (!deflateOK || gzipQuality >= deflateQuality):
		return "gzip"
	case deflateOK && deflateQuality > 0:
		return "deflate"
	default:
		return ""
	}
}

// acceptEncodingQuality 返回某个编码在 Accept-Encoding 中的 q 值以及是否出现。
//
// 规则按 RFC 9110：显式 q=0 表示明确拒绝，所以「出现过」与「可接受」必须分开判断；
// "*" 可作为未列出编码的默认值。解析不了的 q 按 1 处理，避免一个畸形参数
// 让整条连接退回不压缩。
func acceptEncodingQuality(acceptEncoding, encoding string) (float64, bool) {
	var (
		wildcard    float64
		wildcardSet bool
		listed      bool
		quality     float64
	)
	for _, item := range strings.Split(acceptEncoding, ",") {
		fields := strings.Split(strings.TrimSpace(item), ";")
		name := strings.ToLower(strings.TrimSpace(fields[0]))
		if name == "" {
			continue
		}
		itemQuality := 1.0
		for _, parameter := range fields[1:] {
			key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				itemQuality = parsed
			}
		}
		switch {
		case name == encoding:
			listed = true
			quality = itemQuality
		case name == "*":
			wildcardSet = true
			wildcard = itemQuality
		}
	}
	if listed {
		return quality, true
	}
	if wildcardSet {
		return wildcard, true
	}
	return 0, false
}

// addVaryAcceptEncoding 在已有 Vary 上追加 Accept-Encoding，
// 避免同一 URL 的压缩与未压缩响应被中间缓存混用。
func addVaryAcceptEncoding(header http.Header) {
	for _, value := range header.Values("Vary") {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), "Accept-Encoding") {
				return
			}
		}
	}
	header.Add("Vary", "Accept-Encoding")
}
