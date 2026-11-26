package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func serve() error {
	token, err := os.ReadFile(os.Getenv("AGY_BRIDGE_TOKEN_FILE"))
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		return errors.New("AGY_BRIDGE_TOKEN_FILE must contain a strong bridge token")
	}
	model := strings.TrimSpace(os.Getenv("AGY_BRIDGE_MODEL"))
	if model == "" {
		model = "gemini-4-hydrogen-low"
	}
	binary, err := exec.LookPath("agy")
	if err != nil {
		return errors.New("official agy CLI is not installed")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return err
	}
	addr := os.Getenv("AGY_BRIDGE_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8091"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return errors.New("bridge must listen on loopback; use a private authenticated TLS proxy for remote access")
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
	s := &http.Server{Addr: addr, Handler: newBridge(strings.TrimSpace(string(token)), model, cliRunner(binary, work, path, model)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 110 * time.Second, IdleTimeout: 30 * time.Second}
	return s.ListenAndServe()
}
