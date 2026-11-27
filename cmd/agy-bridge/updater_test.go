package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRelease 用一个最小 shell 脚本当假 CLI 二进制：能执行、能按参数打印版本，
// 这样测试能验证「落盘的文件真的可执行」而不需要下载官方包。
func fakeRelease(t *testing.T) (tarGZ []byte, sum string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\ncase \"$1\" in --version) echo 9.9.9;; esac\n"
	if err := tw.WriteHeader(&tar.Header{Name: "antigravity", Mode: 0700, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	h := sha512.Sum512(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(h[:])
}

// fakeOfficial 起一个假的官方服务：清单接口和发布包下载都在同一地址，
// 清单里的 url 也指向本服务，与 pointToOfficial 放行的域前缀保持一致。
// 需要先起服务再写清单，所以 handler 里通过闭包读取自己的地址。
func fakeOfficial(t *testing.T, payload []byte, sum, version string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifests/linux_amd64.json":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"version": version, "sha512": sum,
				"url": srv.URL + "/antigravity-public/" + version + "/cli.tar.gz",
			})
		case "/antigravity-public/" + version + "/cli.tar.gz":
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// pointToOfficial 把清单地址和放行的下载域前缀都指向假服务，测试期间全局生效。
func pointToOfficial(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldBase, oldPrefix := manifestBase, officialDownloadPrefix
	manifestBase = srv.URL
	officialDownloadPrefix = srv.URL + "/antigravity-public/"
	t.Cleanup(func() { manifestBase, officialDownloadPrefix = oldBase, oldPrefix })
}

func TestCLIManagerInstallsVerifiedRelease(t *testing.T) {
	payload, sum := fakeRelease(t)
	pointToOfficial(t, fakeOfficial(t, payload, sum, "9.9.9"))
	m := newCLIManager(filepath.Join(t.TempDir(), "cli"), func() string { return "" })
	if err := m.ensure(t.Context()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got := m.currentVersion(); got != "9.9.9" {
		t.Fatalf("recorded version = %q, want 9.9.9", got)
	}
	// 假二进制是 shell 脚本，只有类 Unix 系统能执行；Windows 本机只验证落盘，
	// 「真的可执行」由 Linux 容器与 TestLiveCLIUpdate 覆盖。
	if runtime.GOOS != "windows" {
		out, err := exec.CommandContext(t.Context(), m.binary(), "--version").CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "9.9.9" {
			t.Fatalf("installed binary: %v %q", err, out)
		}
	}
	if info, err := os.Stat(m.binary()); err != nil || info.Size() == 0 {
		t.Fatalf("binary not written: %v", err)
	}
	// 第二次 ensure 幂等：版本没变不重新下载，不会把正在用的二进制换掉。
	if err := m.ensure(t.Context()); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestCLIManagerRejectsChecksumMismatch(t *testing.T) {
	payload, _ := fakeRelease(t)
	// 清单里的 sha512 故意写成另一个值，模拟传输损坏或清单被篡改。
	pointToOfficial(t, fakeOfficial(t, payload, strings.Repeat("0", 128), "9.9.9"))
	m := newCLIManager(filepath.Join(t.TempDir(), "cli"), func() string { return "" })
	err := m.ensure(t.Context())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	// 校验失败绝不能留下半成品二进制。
	if _, statErr := os.Stat(m.binary()); !os.IsNotExist(statErr) {
		t.Fatal("binary written despite checksum mismatch")
	}
}

func TestCLIManagerRejectsForeignDownloadURL(t *testing.T) {
	_, sum := fakeRelease(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifests/linux_amd64.json" {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"version": "9.9.9", "sha512": sum, "url": "https://evil.example.com/cli.tar.gz",
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	pointToOfficial(t, srv)
	m := newCLIManager(filepath.Join(t.TempDir(), "cli"), func() string { return "" })
	err := m.ensure(t.Context())
	if err == nil || !strings.Contains(err.Error(), "outside the antigravity-public bucket") {
		t.Fatalf("want foreign url rejection, got %v", err)
	}
}

func TestCLIManagerUpgradesWhenManifestChanges(t *testing.T) {
	payload, sum := fakeRelease(t)
	pointToOfficial(t, fakeOfficial(t, payload, sum, "9.9.9"))
	m := newCLIManager(filepath.Join(t.TempDir(), "cli"), func() string { return "" })
	if err := m.ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	// 卷里放一个「旧版本」记录，模拟官方发了新版：再次 install 必须替换。
	if err := os.WriteFile(m.record(), []byte(`{"version":"9.9.8"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.install(t.Context()); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if got := m.currentVersion(); got != "9.9.9" {
		t.Fatalf("after upgrade version = %q, want 9.9.9", got)
	}
}

func TestProxyFuncLeavesLoopbackDirect(t *testing.T) {
	fn, err := proxyFunc("http://proxy.example:3128")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/x", nil)
	if u, _ := fn(r); u != nil {
		t.Fatalf("loopback must bypass proxy, got %v", u)
	}
	r = httptest.NewRequest(http.MethodGet, "https://storage.googleapis.com/x", nil)
	if u, _ := fn(r); u == nil || u.Host != "proxy.example:3128" {
		t.Fatalf("external host must use proxy, got %v", u)
	}
	if _, err = proxyFunc("ftp://x"); err == nil {
		t.Fatal("unsupported scheme accepted")
	}
}
