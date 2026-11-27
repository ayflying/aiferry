package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cliUpdateInterval 是后台检查官方 CLI 新版的间隔。官方发版约一两周一次，
// 每天查一次足够及时，也不至于对清单服务造成无谓压力。
const cliUpdateInterval = 24 * time.Hour

// bridgeVersion 由构建时 -ldflags 注入，与仓库 VERSION 一致；未注入时显示 dev。
var bridgeVersion = "dev"

func main() {
	if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// 默认只允许回环；独立容器须显式开启 IPv4 监听，且不得映射宿主端口。
func allowedListen(addr string, privateDocker bool) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && (host == "127.0.0.1" || host == "::1" || (privateDocker && host == "0.0.0.0"))
}

// serve 启动桥接：先确保可写卷里有最新官方 CLI，再监听 HTTP。
func serve() error {
	stateDir := os.Getenv("AGY_BRIDGE_STATE_DIR")
	if stateDir == "" {
		// 未显式指定时以单账号令牌文件所在目录为状态目录，兼容纯本机部署。
		stateDir = filepath.Dir(os.Getenv("AGY_BRIDGE_TOKEN_FILE"))
	}
	accounts, err := loadAccounts(stateDir)
	if err != nil {
		return err
	}
	model := strings.TrimSpace(os.Getenv("AGY_BRIDGE_MODEL"))
	if model == "" {
		model = "gemini-4-hydrogen-low"
	}
	// 官方 CLI 不在镜像里：启动时从官方清单拉最新版到可写卷，失败即退出，
	// 由容器重启策略重试。镜像不预置旧版兜底——旧协议迟早要更新，留着只会
	// 把问题推迟到排查时才暴露。
	cliDir := os.Getenv("AGY_BRIDGE_CLI_DIR")
	if cliDir == "" {
		cliDir = filepath.Join(stateDir, "cli")
	}
	// 更新器没有「本次请求的代理」可用，下载出口用编排配置的全局代理；
	// 对话请求仍按「请求头 > 账号配置 > 全局代理」的顺序选代理。
	manager := newCLIManager(cliDir, globalProxy)
	installCtx, cancelInstall := context.WithTimeout(context.Background(), 10*time.Minute)
	err = manager.ensure(installCtx)
	cancelInstall()
	if err != nil {
		return fmt.Errorf("official agy cli is required: %w", err)
	}
	binary := manager.binary()
	go manager.loop(context.Background(), cliUpdateInterval)
	addr := os.Getenv("AGY_BRIDGE_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8091"
	}
	if !allowedListen(addr, os.Getenv("AGY_BRIDGE_PRIVATE_DOCKER") == "1") {
		return errors.New("bridge must listen on loopback unless private Docker mode is explicitly enabled")
	}
	// 不从 AiFerry 源码目录或宿主机业务目录启动代理。
	dir, err := os.MkdirTemp("", "aiferry-agy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "guard.json")
	if err := os.WriteFile(path, []byte(guard), 0600); err != nil {
		return err
	}
	work := filepath.Join(dir, "workspace")
	if err := os.Mkdir(work, 0700); err != nil {
		return err
	}
	s := &http.Server{
		Addr: addr, Handler: newBridge(accounts, model, accountRunner(binary, work, path, model), accountUsage(binary, dir), manager),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 110 * time.Second, IdleTimeout: 30 * time.Second,
	}
	return s.ListenAndServe()
}
