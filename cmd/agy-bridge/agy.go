package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"time"
)

const guard = `{"permissions":{"deny":["read_file(*)","write_file(*)","command(*)","mcp(*)","read_url(*)","execute_url(*)","download_url(*)"]}}`

type usage struct {
	Input  int `json:"input_tokens"`
	Output int `json:"output_tokens"`
	Total  int `json:"total_tokens"`
}

type agyEvent struct {
	Event string `json:"event"`
	Step  struct {
		Type string `json:"step_type"`
		Text string `json:"text_delta"`
	} `json:"step_update"`
	Result struct {
		Status string `json:"status"`
		Text   string `json:"response"`
		Usage  usage  `json:"usage"`
	} `json:"result"`
}

func cliRunner(binary, dir, guardPath, model string) runner {
	return func(ctx context.Context, prompt string, emit func(string) error) (usage, error) {
		// 用户输入只能进入 JSON 标准输入，不能改写 CLI 参数或触发斜杠命令。
		data, err := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": prompt}})
		if err != nil {
			return usage{}, err
		}
		ctx, cancel := context.WithTimeout(ctx, 95*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "--input-format", "stream-json", "--output-format", "stream-json", "--disable-slash-commands", "--sandbox", "--settings", guardPath, "--print-timeout", "85s", "--model", model)
		cmd.Dir = dir
		cmd.Stdin = bytes.NewReader(append(data, '\n'))
		cmd.WaitDelay = 2 * time.Second
		out, err := cmd.StdoutPipe()
		if err != nil {
			return usage{}, err
		}
		if err = cmd.Start(); err != nil {
			return usage{}, err
		}
		// 即使 CLI 的子进程还持有管道，取消请求也必须解除扫描阻塞。
		stopClose := context.AfterFunc(ctx, func() { _ = out.Close() })
		defer stopClose()
		u, err := readEvents(out, emit)
		if err != nil {
			cancel()
			_ = out.Close()
		}
		waitErr := cmd.Wait()
		if err != nil {
			return usage{}, err
		}
		return u, waitErr
	}
}

func readEvents(r io.Reader, emit func(string) error) (usage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var (
		u              usage
		streamed, done bool
	)
	for scanner.Scan() {
		var ev agyEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil || done {
			return u, errors.New("invalid agy event stream")
		}
		switch ev.Event {
		case "step_update":
			if ev.Step.Type == "agent_response" && ev.Step.Text != "" {
				if err := emit(ev.Step.Text); err != nil {
					return u, err
				}
				streamed = true
			}
		case "result":
			if ev.Result.Status != "SUCCESS" || (!streamed && ev.Result.Text == "") {
				return u, errors.New("agy did not return a successful text response")
			}
			if !streamed {
				if err := emit(ev.Result.Text); err != nil {
					return u, err
				}
			}
			u, done = ev.Result.Usage, true
		}
	}
	if err := scanner.Err(); err != nil {
		return u, err
	}
	if !done {
		return u, errors.New("incomplete agy response")
	}
	return u, nil
}
