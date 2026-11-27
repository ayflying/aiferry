package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// usageCommandTimeout 覆盖官方 CLI 的 /usage 全程：实测约 8 秒，上限留足余量。
const usageCommandTimeout = 40 * time.Second

// usageCommandResult 对应 `agy -p /usage --output-format stream-json` 的
// command_result 事件（1.5.18 实测）：每个分组含若干额度桶，桶带窗口类型、
// 剩余比例与重置时刻。
type usageCommandResult struct {
	Data struct {
		Groups []struct {
			Name    string `json:"name"`
			Buckets []struct {
				ID        string  `json:"id"`
				Window    string  `json:"window"`
				Remaining float64 `json:"remaining_fraction"`
				Reset     string  `json:"reset_time"`
			} `json:"buckets"`
		} `json:"groups"`
	} `json:"data"`
}

// usagePayload 是桥接对外输出的额度形状，与 AiFerry 内置的 OpenCode Go 用量格式
// 完全一致（usage.rolling = 5 小时窗口，usage.weekly = 每周窗口），因此 AiFerry
// 侧无需新增解析器，复用现有适配器即可。
type usagePayload struct {
	Usage struct {
		Rolling *usageWindow `json:"rolling"`
		Weekly  *usageWindow `json:"weekly"`
	} `json:"usage"`
}

type usageWindow struct {
	Status   string   `json:"status"`
	Percent  *float64 `json:"percent"`
	ResetsAt string   `json:"resetsAt"`
}

// accountUsage 构造「以该账号身份查询订阅额度」的执行器，代理取值规则与
// accountRunner 一致（请求头优先，账号固定代理兜底）。
func accountUsage(binary, dir string) func(account, string) func(context.Context) (usagePayload, error) {
	return func(acc account, requestProxy string) func(context.Context) (usagePayload, error) {
		return func(ctx context.Context) (usagePayload, error) {
			env := baseEnv(acc.home)
			proxy := requestProxy
			if proxy == "" {
				proxy = acc.proxy
			}
			proxyVariables, err := proxyEnv(proxy)
			if err != nil {
				return usagePayload{}, err
			}
			return readUsage(ctx, binary, dir, append(env, proxyVariables...))
		}
	}
}

// readUsage 以该账号的 HOME/代理启动官方 CLI 查询订阅额度。不加 --sandbox：
// 实测沙箱模式下 /usage 会卡在后台任务上直到超时，非沙箱模式约 8 秒返回。
func readUsage(ctx context.Context, binary, dir string, env []string) (usagePayload, error) {
	ctx, cancel := context.WithTimeout(ctx, usageCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-p", "/usage", "--output-format", "stream-json", "--print-timeout", "30s")
	cmd.Dir = dir
	cmd.Env = env
	// /usage 不是对话，不给标准输入送任何事件，避免 CLI 等待交互。
	cmd.Stdin = bytes.NewReader(nil)
	// CLI 的错误输出只进日志（容器内 stdout），不进 HTTP 响应，避免泄露路径或凭据。
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return usagePayload{}, err
	}
	if err = cmd.Start(); err != nil {
		return usagePayload{}, err
	}
	payload, parseErr := parseUsageOutput(out)
	if waitErr := cmd.Wait(); waitErr != nil && parseErr == nil {
		parseErr = fmt.Errorf("agy usage exited: %w", waitErr)
	}
	if parseErr != nil {
		log.Printf("[agy-bridge] /usage failed for account home %s: %v; stderr: %s", dir, parseErr, strings.TrimSpace(stderr.String()))
	}
	return payload, parseErr
}

// parseUsageOutput 逐行扫描 stream-json，取第一条 command_result；解析不出
// 结构化结果时返回错误（不回退到模型自由文本，避免把闲聊当额度）。
func parseUsageOutput(r interface{ Read([]byte) (int, error) }) (usagePayload, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("{")) {
			continue
		}
		var envelope struct {
			Event   string             `json:"event"`
			Command usageCommandResult `json:"command"`
		}
		if json.Unmarshal(line, &envelope) != nil || envelope.Event != "command_result" {
			continue
		}
		payload := usagePayload{}
		for _, group := range envelope.Command.Data.Groups {
			// 只认 Gemini 分组；Claude/GPT 等第三方分组在本桥接不可调用，不展示。
			if !bytes.Contains(bytes.ToLower([]byte(group.Name)), []byte("gemini")) {
				continue
			}
			for _, bucket := range group.Buckets {
				percent := (1 - bucket.Remaining) * 100
				window := usageWindow{Status: "ok", Percent: &percent, ResetsAt: bucket.Reset}
				switch bucket.Window {
				case "weekly":
					payload.Usage.Weekly = &window
				case "5h":
					payload.Usage.Rolling = &window
				}
			}
		}
		if payload.Usage.Rolling == nil && payload.Usage.Weekly == nil {
			return usagePayload{}, errors.New("usage result has no gemini quota bucket")
		}
		return payload, nil
	}
	return usagePayload{}, errors.New("usage command returned no structured result")
}
