package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// proxyHeader 是 AiFerry 转发链路把「该密钥当前选中的代理」带给桥接的内部请求头。
// 只在 Docker 内网、且通过令牌认证之后才会被读取；桥接不回显它。
const proxyHeader = "X-AiFerry-Proxy"

// parseProxyURL 校验代理地址并返回解析结果；空地址返回 nil 表示直连。
// 子进程环境变量与桥接自身的 HTTP 客户端共用这一套规则，避免两处各写一遍。
func parseProxyURL(proxy string) (*url.URL, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return nil, nil
	}
	parsed, err := url.Parse(proxy)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("invalid proxy URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("unsupported proxy scheme")
	}
	return parsed, nil
}

// proxyFunc 把代理地址换算成 http.Transport 需要的形式，供桥接自己访问
// 官方清单与下载地址时使用；回环地址永远不走代理。
func proxyFunc(proxy string) (func(*http.Request) (*url.URL, error), error) {
	parsed, err := parseProxyURL(proxy)
	if err != nil {
		return nil, err
	}
	return func(r *http.Request) (*url.URL, error) {
		if r.URL.Hostname() == "localhost" || r.URL.Hostname() == "127.0.0.1" {
			return nil, nil
		}
		return parsed, nil
	}, nil
}

// proxyEnv 把代理地址换算成官方 agy 进程的环境变量。CLI 自己遵循标准的
// HTTPS_PROXY / HTTP_PROXY / NO_PROXY 约定，因此不改它的任何行为。
// NO_PROXY 继承编排里配置的内网例外（主服务要连 MySQL/Redis/Casdoor），
// 再固定加上回环地址：桥接自身与容器内健康检查永远不走代理。
func proxyEnv(proxy string) ([]string, error) {
	parsed, err := parseProxyURL(proxy)
	if err != nil || parsed == nil {
		return nil, err
	}
	noProxy := "localhost,127.0.0.1,::1"
	if extra := strings.TrimSpace(os.Getenv("NO_PROXY")); extra != "" {
		noProxy += "," + extra
	}
	return []string{
		"HTTPS_PROXY=" + proxy, "HTTP_PROXY=" + proxy, "https_proxy=" + proxy, "http_proxy=" + proxy,
		"NO_PROXY=" + noProxy, "no_proxy=" + noProxy,
	}, nil
}

// globalProxy 是编排里配置的全局出口：优先读桥接专用变量 AGY_BRIDGE_PROXY，
// 没有时退回标准的 HTTPS_PROXY / HTTP_PROXY，这样编排只配一份代理就能同时
// 覆盖「桥接下载官方 CLI」与「渠道未配代理时的对话出网」。没配置即直连。
func globalProxy() string {
	for _, name := range []string{"AGY_BRIDGE_PROXY", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// baseEnv 是官方 agy 进程允许继承的全部环境：只保留网络出口与语言/时区类变量，
// 其余宿主变量一概不传，避免把容器内的其他配置带进 CLI。
// HOME 与 USERPROFILE 同时指向账号目录：Linux 容器只认 HOME，Windows 版 CLI
// 只认 USERPROFILE，两处一致才能保证「账号目录 = 登录态目录」在任何平台成立。
func baseEnv(home string) []string {
	env := []string{"HOME=" + home, "USERPROFILE=" + home, "PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "TERM=dumb"}
	for _, name := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR", "TZ"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// accountRunner 构造「以该账号身份调用官方 CLI」的执行器：HOME 指向账号目录。
// 代理优先级：请求头（AiFerry 按密钥选中的代理）> 账号固定配置 > 编排全局代理；
// 三处都没配即直连。
func accountRunner(binary, work, guardPath, model string) func(account, string) runner {
	return func(acc account, requestProxy string) runner {
		return func(ctx context.Context, prompt string, emit func(string) error) (usage, error) {
			env := baseEnv(acc.home)
			proxy := requestProxy
			if proxy == "" {
				proxy = acc.proxy
			}
			if proxy == "" {
				proxy = globalProxy()
			}
			proxyVariables, err := proxyEnv(proxy)
			if err != nil {
				return usage{}, err
			}
			env = append(env, proxyVariables...)
			return runCLI(ctx, binary, work, guardPath, model, env, prompt, emit)
		}
	}
}
