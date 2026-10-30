package cluster

import (
	"context"
	"net/http"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	// pushDebounce 合并短时间内的连续变更（一次保存往往写多张表）。
	pushDebounce = 300 * time.Millisecond
	// pushRetries 是单节点推送的重试次数，失败后由子节点条件轮询兜底。
	pushRetries = 3
	// pushTimeoutTotal 是一次推送批次的总时限。
	pushTimeoutTotal = 15 * time.Second
	// notifyRequestTimeout 是单个节点通知的时限。
	notifyRequestTimeout = 3 * time.Second

	contentTypeJSON = "application/json"
	contentTypeGzip = "application/gzip"
)

// postNotify 向子节点发送「配置版本已变」的通知，成功判定为 2xx。
//
// 这里刻意不携带快照内容：通知只是唤醒信号，子节点随后自己按版本号条件拉取，
// 这样即使推送丢失或乱序，子节点状态仍会收敛。
func (s *Service) postNotify(ctx context.Context, node Node, body []byte, version int64) error {
	requestCtx, cancel := context.WithTimeout(ctx, notifyRequestTimeout)
	defer cancel()
	response, err := s.call(requestCtx, node.Address, "/notify", body, contentTypeJSON)
	if err != nil {
		return err
	}
	if response == nil {
		return gerror.New("cluster: empty notify response")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := readLimited(response)
		return gerror.Newf("cluster: notify node %s failed with %d: %s",
			node.Name, response.StatusCode, payload)
	}
	return nil
}

// readLimited 读取响应体用于错误信息，最多 512 字节，避免日志被撑爆。
func readLimited(response *http.Response) (string, error) {
	buffer := make([]byte, 512)
	count, err := response.Body.Read(buffer)
	if count == 0 {
		return "", err
	}
	return string(buffer[:count]), nil
}

// clusterLogger 在未注入 logger 时回退到 GoFrame 全局日志。
func (s *Service) clusterLogger() Logger {
	if s.logger != nil {
		return s.logger
	}
	return globalLogger{}
}

type globalLogger struct{}

func (globalLogger) Warningf(ctx context.Context, format string, args ...any) {
	g.Log().Warningf(ctx, format, args...)
}

func (globalLogger) Infof(ctx context.Context, format string, args ...any) {
	g.Log().Infof(ctx, format, args...)
}
