package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
)

// proxyHeader 是 AiFerry 转发链路把「该密钥当前选中的代理」带给桥接的内部请求头。
// 只在 Docker 内网、且通过令牌认证之后才会被读取；桥接不回显它。
const proxyHeader = "X-AiFerry-Proxy"

// proxyEnv 把代理地址换算成官方 agy 进程的环境变量。CLI 自己遵循标准的
// HTTPS_PROXY / HTTP_PROXY / NO_PROXY 约定，因此不改它的任何行为；
// 本机回环地址（桥接自身、容器内的健康检查）永远不走代理。
func proxyEnv(proxy string) ([]string, error) {
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
	return []string{
		"HTTPS_PROXY=" + proxy, "HTTP_PROXY=" + proxy, "https_proxy=" + proxy, "http_proxy=" + proxy,
		"NO_PROXY=localhost,127.0.0.1,::1", "no_proxy=localhost,127.0.0.1,::1",
	}, nil
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

// accountRunner 构造「以该账号身份调用官方 CLI」的执行器：HOME 指向账号目录，
// 代理来自账号固定配置或请求头（请求头优先，便于 AiFerry 按密钥配对代理时如实生效）。
func accountRunner(binary, work, guardPath, model string) func(account, string) runner {
	return func(acc account, requestProxy string) runner {
		return func(ctx context.Context, prompt string, emit func(string) error) (usage, error) {
			env := baseEnv(acc.home)
			proxy := requestProxy
			if proxy == "" {
				proxy = acc.proxy
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
