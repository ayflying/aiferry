package app

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

type sApp struct {
	Config  config.App
	Redis   cache.Cache
	Secrets *secret.Service
	HTTP    *http.Client
	// HTTPDirect 是不走环境代理的客户端，用于上游直连场景。
	HTTPDirect *http.Client
}

// New 按角色装配进程依赖：主节点连 MySQL/Redis，子节点只开内置 SQLite。
func New(ctx context.Context, cfg config.App) (*sApp, error) {
	secrets, err := secret.New(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	shared, direct := newHTTPClients()
	service := &sApp{Config: cfg, Secrets: secrets, HTTP: shared, HTTPDirect: direct}
	if cfg.IsWorker() {
		err = service.openWorker(ctx)
	} else {
		err = service.openMaster(ctx)
	}
	if err != nil {
		return nil, err
	}
	return service, nil
}

// newHTTPClients 构造上游转发与直连两个客户端，二者共享连接池配置。
func newHTTPClients() (*http.Client, *http.Client) {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		// 长文本生成（长上下文分镜/写作）上游非流式首包普遍超过 180s，
		// 触发 http2: timeout awaiting response headers 后以 502 回源。
		// 提高到 600s 覆盖重负载场景；流式请求本就不受此限制（响应头秒回）。
		ResponseHeaderTimeout: 600 * time.Second,
	}
	directTransport := transport.Clone()
	directTransport.Proxy = nil
	return &http.Client{Transport: transport}, &http.Client{Transport: directTransport}
}