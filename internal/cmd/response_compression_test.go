package cmd

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"testing"
)

// 压缩逻辑与 GoFrame 的 Response 对象解耦，可以直接用 io 缓冲验证：
// 本文件只测「给定响应体与客户端偏好，压缩后应当得到什么」，
// 钩子与真实服务器生命周期的接线由集成验证覆盖。

// compressibleJSON 造一份足够大、可压缩的模拟转发响应体。
func compressibleJSON(repeat int) []byte {
	return []byte(`{"data":"` + strings.Repeat("aiferry", repeat) + `"}`)
}

// TestNegotiatedCompressionEncoding 覆盖 Accept-Encoding 的 q 值协商。
func TestNegotiatedCompressionEncoding(t *testing.T) {
	for _, testCase := range []struct {
		acceptEncoding string
		want           string
	}{
		{acceptEncoding: "", want: ""},
		{acceptEncoding: "gzip", want: "gzip"},
		{acceptEncoding: "deflate", want: "deflate"},
		{acceptEncoding: "gzip;q=0", want: ""},
		{acceptEncoding: "gzip;q=0.2, deflate;q=0.9", want: "deflate"},
		{acceptEncoding: "br", want: ""},
		{acceptEncoding: "*", want: "gzip"},
		{acceptEncoding: "identity", want: ""},
		{acceptEncoding: "GZIP", want: "gzip"},
		{acceptEncoding: "gzip, deflate, br, zstd", want: "gzip"},
	} {
		if got := negotiatedCompressionEncoding(testCase.acceptEncoding); got != testCase.want {
			t.Fatalf("negotiatedCompressionEncoding(%q) = %q, want %q", testCase.acceptEncoding, got, testCase.want)
		}
	}
}

// TestCompressResponseBodyGzip 验证 gzip 压缩结果可解压且确实更小。
func TestCompressResponseBodyGzip(t *testing.T) {
	body := compressibleJSON(2000)
	compressed, err := compressResponseBody(body, "gzip")
	if err != nil {
		t.Fatalf("compress gzip: %v", err)
	}
	if len(compressed) >= len(body) {
		t.Fatalf("compressed %d bytes not smaller than plain %d bytes", len(compressed), len(body))
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("open gzip body: %v", err)
	}
	defer func() { _ = reader.Close() }()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("decode gzip body: %v", err)
	}
	if !bytes.Equal(decoded, body) {
		t.Fatalf("decoded body differs from plain body")
	}
}

// TestCompressResponseBodyDeflate 验证 deflate 压缩结果可解压且确实更小。
func TestCompressResponseBodyDeflate(t *testing.T) {
	body := compressibleJSON(2000)
	compressed, err := compressResponseBody(body, "deflate")
	if err != nil {
		t.Fatalf("compress deflate: %v", err)
	}
	if len(compressed) >= len(body) {
		t.Fatalf("compressed %d bytes not smaller than plain %d bytes", len(compressed), len(body))
	}
	inflater := flate.NewReader(bytes.NewReader(compressed))
	defer func() { _ = inflater.Close() }()
	decoded, err := io.ReadAll(inflater)
	if err != nil {
		t.Fatalf("decode deflate body: %v", err)
	}
	if !bytes.Equal(decoded, body) {
		t.Fatalf("decoded body differs from plain body")
	}
}

// TestCompressResponseBodyUnknownEncoding 验证未知编码返回空结果而不是报错。
func TestCompressResponseBodyUnknownEncoding(t *testing.T) {
	for _, encoding := range []string{"", "br", "identity"} {
		if got, err := compressResponseBody([]byte("body"), encoding); err != nil || got != nil {
			t.Fatalf("compressResponseBody(%q) = %v, %v", encoding, got, err)
		}
	}
}

// TestAddVaryAcceptEncoding 验证 Vary 不重复追加。
func TestAddVaryAcceptEncoding(t *testing.T) {
	header := http.Header{}
	addVaryAcceptEncoding(header)
	addVaryAcceptEncoding(header)
	header.Add("Vary", "Origin")
	addVaryAcceptEncoding(header)
	if values := header.Values("Vary"); len(values) != 2 {
		t.Fatalf("Vary = %v, want exactly two entries", values)
	}
}

// TestCompressResponseBodyKeepsHighEntropy 说明高熵内容压缩后不会变小，
// 这是钩子选择「压缩后更大就原样输出」的依据。
func TestCompressResponseBodyKeepsHighEntropy(t *testing.T) {
	random := make([]byte, 8192)
	source := rand.New(rand.NewSource(1))
	if _, err := source.Read(random); err != nil {
		t.Fatalf("build random body: %v", err)
	}
	compressed, err := compressResponseBody(random, "gzip")
	if err != nil {
		t.Fatalf("compress random body: %v", err)
	}
	if len(compressed) < len(random) {
		t.Fatalf("high entropy body compressed from %d to %d bytes, fixture assumption broken", len(random), len(compressed))
	}
}