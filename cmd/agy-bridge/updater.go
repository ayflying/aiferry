package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// manifestBase 是官方 CLI 的版本清单服务：返回 {version, url, sha512}。桥接只
// 从这里取下载地址与校验和，不自己拼官方存储路径——版本号里的构建 ID 会变。
// 声明为变量仅供测试替换成 httptest 服务；生产代码不改它。
var manifestBase = "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app"

// officialDownloadPrefix 限定发布包只能来自官方存储桶，防止清单被篡改后把
// 任意地址的二进制写进卷。单测通过替换它来指向本地假发布包。
var officialDownloadPrefix = "https://storage.googleapis.com/antigravity-public/"

// manifest 是官方清单接口的返回体，字段与官方 install.sh 解析的三个键一致。
type manifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA512  string `json:"sha512"`
}

// cliRelease 是落盘后的版本记录：下次启动或定期检查时据此判断是否需要更新，
// 也供 /v1/version 回显，不必每次都去跑 agy --version。
type cliRelease struct {
	Version string `json:"version"`
}

const binaryName = "antigravity"

// cliManager 负责把最新官方 CLI 维护在可写卷里：启动时必须成功，之后定期
// 检查更新。镜像里不预置任何版本——旧版 CLI 的协议与当前服务端可能不兼容，
// 留一份兜底只会把问题推迟到排查时才暴露，不如一开始就跑最新。
type cliManager struct {
	dir     string
	client  *http.Client
	proxy   func() string
	version func() string
}

func newCLIManager(dir string, proxy func() string) *cliManager {
	return &cliManager{dir: dir, client: &http.Client{Timeout: 2 * time.Minute}, proxy: proxy, version: func() string { return "" }}
}

// binary 返回卷内 CLI 的可执行文件路径。
func (m *cliManager) binary() string { return filepath.Join(m.dir, binaryName) }

// record 是版本记录文件路径，与二进制同目录，保证 rename 时一起可见。
func (m *cliManager) record() string { return filepath.Join(m.dir, "release.json") }

// currentVersion 读取卷内记录的版本号；没有记录返回空串。
func (m *cliManager) currentVersion() string {
	data, err := os.ReadFile(m.record())
	if err != nil {
		return ""
	}
	var rel cliRelease
	if json.Unmarshal(data, &rel) != nil {
		return ""
	}
	return rel.Version
}

// ensure 是启动路径：卷里没有可用 CLI 就必须装上最新版，失败即返回错误让
// 容器退出重启——不做降级，不用旧版顶替。
func (m *cliManager) ensure(ctx context.Context) error {
	if v := m.currentVersion(); v != "" {
		if _, err := os.Stat(m.binary()); err == nil {
			// 已有版本：后台再比对一次清单，有新版就替换；不阻塞服务启动。
			return nil
		}
	}
	return m.install(ctx)
}

// install 拉取官方清单，下载、校验、原子替换卷内二进制。流程与官方
// install.sh 一致：清单 → 下载 → sha512 比对 → 解包 antigravity → 落盘。
func (m *cliManager) install(ctx context.Context) error {
	rel, err := m.fetchManifest(ctx)
	if err != nil {
		return err
	}
	if rel.Version == m.currentVersion() {
		if _, err := os.Stat(m.binary()); err == nil {
			return nil
		}
	}
	payload, err := m.download(ctx, rel.URL)
	if err != nil {
		return err
	}
	defer os.Remove(payload)
	if err = verifySHA512(payload, rel.SHA512); err != nil {
		return err
	}
	if err = os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	// 临时文件与目标同目录：Linux 下同卷 rename 是原子的，正在运行的旧进程
	// 不受影响，新请求直接拿到新版，不会读到写了一半的二进制。
	tmp, err := os.CreateTemp(m.dir, ".cli-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = extractBinary(payload, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0700); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), m.binary()); err != nil {
		return err
	}
	rec, err := json.Marshal(cliRelease{Version: rel.Version})
	if err != nil {
		return err
	}
	if err = os.WriteFile(m.record(), rec, 0600); err != nil {
		return err
	}
	log.Printf("official agy cli installed: version=%s", rel.Version)
	return nil
}

// fetchManifest 读取当前平台的官方清单。平台写死 linux/amd64：桥接镜像只有
// 这一种构建目标，避免在运行时探测架构引入分叉。
func (m *cliManager) fetchManifest(ctx context.Context) (manifest, error) {
	var rel manifest
	url := manifestBase + "/manifests/linux_amd64.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return rel, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// 下载走账号配置的代理：国内宿主机直连 Google 多半不通，与对话请求同一条出口。
	if p := m.proxy(); p != "" {
		if transport.Proxy, err = proxyFunc(p); err != nil {
			return rel, err
		}
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return rel, fmt.Errorf("fetch official cli manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rel, fmt.Errorf("official cli manifest returned status %d", resp.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&rel); err != nil {
		return rel, fmt.Errorf("decode official cli manifest: %w", err)
	}
	if rel.Version == "" || rel.URL == "" || rel.SHA512 == "" {
		return rel, errors.New("official cli manifest is missing version, url or sha512")
	}
	// 只接受官方存储域，防止清单被篡改后把任意地址的二进制写进卷。
	if !strings.HasPrefix(rel.URL, officialDownloadPrefix) {
		return rel, errors.New("official cli manifest url is outside the antigravity-public bucket")
	}
	return rel, nil
}

// download 把发布包下载到临时文件；返回路径由调用方负责清理。
func (m *cliManager) download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if p := m.proxy(); p != "" {
		if transport.Proxy, err = proxyFunc(p); err != nil {
			return "", err
		}
	}
	resp, err := (&http.Client{Transport: transport, Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", fmt.Errorf("download official cli: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("official cli download returned status %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "agy-release-*")
	if err != nil {
		return "", err
	}
	if _, err = io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}

// verifySHA512 按官方清单校验发布包；不匹配即拒绝，不写入卷。
func verifySHA512(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha512.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(want) {
		return errors.New("official cli checksum mismatch")
	}
	return nil
}

// extractBinary 把发布包里的 CLI 二进制写入 out。官方发布包是 tar.gz 且只含
// 一个 antigravity 文件；按 gzip 魔数判断格式，与官方 install.sh 对非 tar.gz
// 发布包「整包即二进制」的处理保持一致。不整包解压，避免把其他内容带进卷。
func extractBinary(payload string, out *os.File) error {
	f, err := os.Open(payload)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		// 不是 gzip：按官方脚本逻辑，非 tar.gz 的发布包就是裸二进制。
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err = io.Copy(out, f)
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("antigravity binary not found in release archive")
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == binaryName && hdr.Typeflag == tar.TypeReg {
			_, err = io.Copy(out, tr)
			return err
		}
	}
}

// loop 每隔 interval 检查一次官方清单；更新失败只记日志，不打断服务。
func (m *cliManager) loop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 10*time.Minute)
			if err := m.install(c); err != nil {
				log.Printf("official agy cli update skipped: %v", err)
			}
			cancel()
		}
	}
}
