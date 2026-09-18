package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadLimitedBodyWithinAndOverLimit(t *testing.T) {
	body, exceeded, err := readLimitedBody(strings.NewReader(strings.Repeat("x", 1024)), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exceeded {
		t.Fatalf("body of exactly limit bytes must not be reported as exceeded")
	}
	if len(body) != 1024 {
		t.Fatalf("body length = %d, want 1024", len(body))
	}

	body, exceeded, err = readLimitedBody(strings.NewReader(strings.Repeat("x", 1025)), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exceeded {
		t.Fatalf("body above limit must be reported as exceeded")
	}
	if len(body) != 1025 {
		t.Fatalf("body length = %d, want 1025", len(body))
	}
}

// GoFrame 在 ServeHTTP 里用 http.MaxBytesReader 包住 r.Body（默认 8MiB），
// 超限时底层 Read 返回 *http.MaxBytesError —— 这正是线上 400
// "Unable to read request body" 的来源，必须映射为带真实上限的 413。
func TestClassifyMaxBytesErrorReportsGatewayLimit(t *testing.T) {
	reader := http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(strings.NewReader(strings.Repeat("x", 4096))), 1024)
	_, _, err := readLimitedBody(reader, 8<<20)
	if err == nil {
		t.Fatalf("expected MaxBytesReader to fail on oversized body")
	}
	var maxBytes *http.MaxBytesError
	if !errors.As(err, &maxBytes) {
		t.Fatalf("expected *http.MaxBytesError, got %v", err)
	}

	failure := classifyBodyReadFailure(err, 8<<20)
	if failure.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", failure.status, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(failure.message, humanByteSize(maxBytes.Limit)) {
		t.Fatalf("message %q should name the limit %q", failure.message, humanByteSize(maxBytes.Limit))
	}
}

func TestClassifyBodyReadFailure(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantText   string
	}{
		{"truncated upload", io.ErrUnexpectedEOF, http.StatusBadRequest, "retry"},
		{"request context canceled", context.Canceled, http.StatusBadRequest, "retry"},
		{"closed connection", net.ErrClosed, http.StatusBadRequest, "retry"},
		{"connection reset by peer", errors.New("read tcp 10.0.0.1:9999->10.0.0.2:1: connection reset by peer"), http.StatusBadRequest, "retry"},
		{"unknown error keeps legacy wording", errors.New("boom"), http.StatusBadRequest, "Unable to read request body"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			failure := classifyBodyReadFailure(testCase.err, maxChatRequestBodyLimit)
			if failure.status != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", failure.status, testCase.wantStatus)
			}
			if failure.kind != "invalid_request_error" {
				t.Fatalf("kind = %q, want invalid_request_error", failure.kind)
			}
			if !strings.Contains(failure.message, testCase.wantText) {
				t.Fatalf("message = %q, want to contain %q", failure.message, testCase.wantText)
			}
		})
	}
}

func TestBodyLimitFailureNamesTheLimit(t *testing.T) {
	failure := bodyLimitFailure(maxChatRequestBodyLimit)
	if failure.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", failure.status, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(failure.message, "32 MiB") {
		t.Fatalf("message = %q, want to name 32 MiB", failure.message)
	}
}

func TestHumanByteSize(t *testing.T) {
	cases := map[int64]string{
		0:                        "configured",
		maxVideoRequestBodyLimit: "64 MiB",
		1024:                     "1024 bytes",
	}
	for limit, want := range cases {
		if got := humanByteSize(limit); got != want {
			t.Fatalf("humanByteSize(%d) = %q, want %q", limit, got, want)
		}
	}
}

// 端点上限必须严格小于 GoFrame 的 server.clientMaxBodySize，
// 否则请求会在控制器读取之前就被拦掉（表现为 400 读取失败）。
func TestGatewayBodyCapCoversEndpointLimits(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "manifest", "config", "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	cap := configuredClientMaxBodySize(t, string(content))

	limits := map[string]int64{
		"chat":   maxChatRequestBodyLimit,
		"audio":  maxAudioRequestBodyLimit,
		"images": maxImagesRequestBodyLimit,
		"video":  maxVideoRequestBodyLimit,
	}
	for name, limit := range limits {
		if limit >= cap {
			t.Fatalf("%s endpoint limit %s must be smaller than clientMaxBodySize %s", name, humanByteSize(limit), humanByteSize(cap))
		}
	}
}

func configuredClientMaxBodySize(t *testing.T, config string) int64 {
	t.Helper()
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "clientMaxBodySize:") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "clientMaxBodySize:")), `"'`)
		multiplier := int64(1)
		if suffix := "kb"; strings.HasSuffix(value, suffix) {
			multiplier, value = 1<<10, strings.TrimSuffix(value, suffix)
		} else if suffix := "m"; strings.HasSuffix(value, suffix) {
			multiplier, value = 1<<20, strings.TrimSuffix(value, suffix)
		}
		size, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatalf("parse clientMaxBodySize %q: %v", value, err)
		}
		return size * multiplier
	}
	t.Fatalf("clientMaxBodySize is not configured in manifest/config/config.yaml")
	return 0
}
