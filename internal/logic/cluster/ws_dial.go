package cluster

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gorilla/websocket"

	"github.com/yunloli/aiferry/internal/buildinfo"
)

// 子节点侧 WS 连接器的重连节奏。
const (
	// wsDialTimeout 是建立连接（含 TCP、TLS、WS 握手）的总时限。
	wsDialTimeout = 15 * time.Second
	// wsReconnectBase 是首次重连等待，之后指数退避。
	wsReconnectBase = time.Second
	// wsReconnectMax 是退避上限，避免主节点长时间不可用时形成重连风暴。
	wsReconnectMax = 30 * time.Second
)

// errNoMasterURL 在子节点未配置主节点地址时返回，调用方据此跳过连接。
var errNoMasterURL = gerror.New("cluster: master url is not configured")

// wsConnectLoop 维护与主节点的长连接，直到 ctx 结束。
//
// 它承担三件事，都只围绕「通知」：
//  1. 建链后把主节点返回的版本号交给 NotifyConfigVersion，落后就立即补一次同步；
//  2. 收到 notify 时唤醒同步，与 HTTP 推送路径共用同一个合并通道；
//  3. 断线后指数退避重连，期间完全依赖原有的定时轮询兜底，不影响正确性。
func (s *Service) wsConnectLoop(ctx context.Context) {
	backoff := wsReconnectBase
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		if err := s.serveStreamOnce(ctx); err != nil && ctx.Err() == nil {
			s.clusterLogger().Warningf(ctx, "cluster: config stream: %v", err)
		}
		// 连接维持过一段时间说明链路正常，退避从头开始算；
		// 否则保留上次退避继续放大，防止「连上就断」的抖动打成高频重连。
		if time.Since(started) > wsPongWait {
			backoff = wsReconnectBase
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > wsReconnectMax {
			backoff = wsReconnectMax
		}
	}
}

// serveStreamOnce 建立一次连接并阻塞到连接结束。
//
// 握手仍走标准 HTTP 请求，因此可以复用既有 HMAC 签名：
// 签名路径是「/internal/cluster/stream」，GET，body 为空。
func (s *Service) serveStreamOnce(ctx context.Context) error {
	master := s.config.Cluster.MasterURL
	if master == "" {
		return errNoMasterURL
	}
	endpoint, err := wsEndpoint(master, wsPath)
	if err != nil {
		return err
	}
	signPath, err := signPathOf(wsPath)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, wsDialTimeout)
	defer cancel()
	headers, err := s.signer.Headers(s.NodeName(), http.MethodGet, signPath, nil)
	if err != nil {
		return err
	}
	// 节点版本走自定义头：握手头不参与签名，只用于主节点展示与排查，
	// 因此不能被当作鉴权依据使用。
	headers.Set("X-Aiferry-Version", buildinfo.RuntimeVersion())
	dialer := websocket.Dialer{HandshakeTimeout: wsDialTimeout, Proxy: http.ProxyFromEnvironment}
	conn, response, err := dialer.DialContext(dialCtx, endpoint, headers)
	if err != nil {
		// 主节点拒绝订阅（未启用集群、验签失败、节点被吊销）时，
		// 只报状态码不报响应体，避免把内部错误细节写进子节点日志。
		if response != nil {
			return &wsDeniedError{status: response.StatusCode}
		}
		return err
	}
	defer func() { _ = conn.Close() }()
	configureStreamReader(conn, wsPongWait, wsWriteTimeout)
	s.clusterLogger().Infof(ctx, "cluster: subscribed to master config stream")

	// 取消服务上下文时主动关闭连接，唤醒阻塞中的 ReadJSON；不能只等读超时。
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()

	for {
		message := wsMessage{}
		if err = conn.ReadJSON(&message); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
		switch message.Type {
		case wsMessageNotify, wsMessageState:
			// 两条路径都收敛到 NotifyConfigVersion：版本不大于本地时它是空操作，
			// 因此重复通知、乱序通知都不会造成多余的全量拉取。
			if err = s.NotifyConfigVersion(ctx, message.Version); err != nil && ctx.Err() == nil {
				s.clusterLogger().Warningf(ctx, "cluster: apply streamed version %d: %v", message.Version, err)
			}
		default:
			s.clusterLogger().Warningf(ctx, "cluster: unknown stream message type %q", message.Type)
		}
	}
}

// wsDeniedError 表示主节点明确拒绝了订阅，而不是网络错误。
type wsDeniedError struct {
	status int
}

func (e *wsDeniedError) Error() string {
	return "cluster: master rejected config stream with HTTP " + strconv.Itoa(e.status)
}

// wsEndpoint 把主节点 HTTP 基地址换成 WS 地址。
//
// 端口刻意复用 HTTP 端口：子节点不需要额外对外开放端口，
// 反向代理只要转发同一条路径即可，部署清单也不用改。
func wsEndpoint(master, path string) (string, error) {
	parsed, err := url.Parse(master)
	if err != nil {
		return "", gerror.Wrap(err, "parse master url for websocket")
	}
	if parsed.Host == "" {
		return "", gerror.New("cluster: master url has no host")
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", gerror.New("cluster: master url must be http or https")
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
