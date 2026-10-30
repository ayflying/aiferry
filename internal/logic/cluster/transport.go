package cluster

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// workerPath 是主节点上的集群内部接口前缀。
const workerPathPrefix = "/internal/cluster"

// TriggerPush 异步触发一次向所有子节点的推送。
//
// 去抖原因：一次「保存渠道」往往连写渠道、模型、凭据三张表，连续 3 次 Mark 不必
// 让子节点拉 3 次快照，合并成一次即可（快照始终是全量最新状态）。
func (s *Service) TriggerPush() {
	if !s.Enabled() || s.IsWorker() {
		return
	}
	s.pushMu.Lock()
	if s.pushPending {
		s.pushMu.Unlock()
		return
	}
	s.pushPending = true
	s.pushMu.Unlock()

	go func() {
		timer := time.NewTimer(pushDebounce)
		defer func() {
			timer.Stop()
			s.pushMu.Lock()
			s.pushPending = false
			s.pushMu.Unlock()
		}()
		<-timer.C
		ctx, cancel := context.WithTimeout(context.Background(), pushTimeoutTotal)
		defer cancel()
		if err := s.pushToNodes(ctx); err != nil && s.logger != nil {
			s.logger.Warningf(ctx, "push cluster config change: %v", err)
		}
	}()
}

// pushToNodes 通知所有启用节点「配置版本变了」。失败不影响主节点，子节点会在
// 下一次条件轮询时补齐，因此这里只记日志。
func (s *Service) pushToNodes(ctx context.Context) error {
	version, err := s.ConfigVersion(ctx)
	if err != nil {
		return err
	}
	nodes, err := s.ActiveNodes(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Address == "" {
			continue
		}
		if err = s.notifyNode(ctx, node, version); err != nil && s.logger != nil {
			s.logger.Warningf(ctx, "notify node %s (%s): %v", node.Name, node.Address, err)
		}
	}
	return nil
}

// notifyNode 向单个子节点推送版本号，失败重试三次（指数退避）。
func (s *Service) notifyNode(ctx context.Context, node Node, version int64) error {
	body, err := json.Marshal(map[string]int64{"version": version})
	if err != nil {
		return gerror.Wrap(err, "encode notify payload")
	}
	var lastErr error
	for attempt := 0; attempt < pushRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		lastErr = s.postNotify(ctx, node, body, version)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// call 发起一次带签名的集群内部请求。path 必须已包含查询串（签名只覆盖 URL.Path）。
func (s *Service) call(ctx context.Context, baseURL, path string, body []byte, contentType string) (*http.Response, error) {
	signPath, err := signPathOf(path)
	if err != nil {
		return nil, err
	}
	endpoint := baseURL + workerPathPrefix + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, gerror.Wrap(err, "build cluster request")
	}
	headers, err := s.signer.Headers(s.NodeName(), http.MethodPost, signPath, body)
	if err != nil {
		return nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Accept-Encoding", "gzip")
	return s.client.Do(request)
}

// decodeGzipJSON 读取响应体并按需解压。主节点快照用 gzip 压缩传输。
func decodeGzipJSON(response *http.Response, target any) error {
	defer func() { _ = response.Body.Close() }()
	var reader io.Reader = response.Body
	if response.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(response.Body)
		if err != nil {
			return gerror.Wrap(err, "open gzip response")
		}
		defer func() { _ = gzipReader.Close() }()
		reader = gzipReader
	}
	if err := json.NewDecoder(reader).Decode(target); err != nil {
		return gerror.Wrap(err, "decode cluster response")
	}
	return nil
}

// signPathOf 从带查询串的 path 中取出用于签名的部分。
func signPathOf(path string) (string, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return "", gerror.Wrap(err, "parse cluster path")
	}
	return parsed.Path, nil
}

// clusterNodeAddress 规范化节点上报地址：只允许 http/https，去掉结尾斜杠。
func clusterNodeAddress(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", gerror.Wrap(err, "parse node address")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", gerror.New("cluster: node address must use http or https")
	}
	if parsed.Host == "" {
		return "", gerror.New("cluster: node address must include host")
	}
	if host, _, splitErr := net.SplitHostPort(parsed.Host); splitErr == nil && host == "" {
		return "", gerror.New("cluster: node address host is empty")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// parseInt64 解析请求参数，空值返回 0。
func parseInt64(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, gerror.Wrap(err, "parse integer parameter")
	}
	return value, nil
}
