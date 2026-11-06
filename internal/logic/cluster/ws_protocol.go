package cluster

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket 通知通道的协议常量。
//
// 设计边界（2026-11-06 定案）：WS 只承担「主节点向子节点推送配置版本变更」这一个方向，
// 快照、用量回传、心跳仍走原有 HTTP 接口。原因有三：
//   - 快照是 gzip 大包，走 WS 要自己实现分片与背压，收益为零；
//   - 请求-响应模型自带状态码、超时与重试，排查问题看日志即可；
//   - 现有 HMAC 签名覆盖「方法 + 路径 + body 摘要」，WS 握手后的自定义帧没有这层保护。
const (
	// wsPath 是主节点上的订阅端点，与其它内部接口共用同一个 HTTP 端口。
	// 端口不单独开放：AIFERRY_MASTER_URL 指到哪个端口，WS 就用哪个端口。
	wsPath = workerPathPrefix + "/stream"

	// wsMessageNotify 是主节点下发的唯一消息类型：配置版本已变。
	wsMessageNotify = "notify"
	// wsMessageState 是主节点返回的订阅确认，携带当前版本号，便于子节点判断是否落后。
	wsMessageState = "state"

	// wsWriteTimeout 是单条消息的写超时，防止对端不读导致协程挂死。
	wsWriteTimeout = 5 * time.Second
	// wsPongWait 允许的最长静默时间。主节点每 wsPingInterval 发一次 ping，
	// 中间层（NAT、负载均衡）通常 60 秒无流量就断链，这里取 50 秒留出余量。
	wsPongWait     = 50 * time.Second
	wsPingInterval = 25 * time.Second
	// wsReadLimit 限制上行帧大小。子节点不通过这条连接发数据，留一点余量即可。
	wsReadLimit = 8 << 10
)

// configureStreamReader 为双方统一安装控制帧处理器，须在开始读取之前调用。
// 主节点发 ping，子节点回 pong：接收 ping 的一侧也必须刷新读期限，
// 不能只处理 pong，否则空闲但健康的子节点仍会在 wsPongWait 后超时。
// WriteControl 可与唯一的业务写协程并发使用，不改写它的写期限。
func configureStreamReader(conn *websocket.Conn, wait, writeTimeout time.Duration) {
	conn.SetReadLimit(wsReadLimit)
	refresh := func() error { return conn.SetReadDeadline(time.Now().Add(wait)) }
	_ = refresh()
	conn.SetPongHandler(func(string) error { return refresh() })
	conn.SetPingHandler(func(data string) error {
		if err := refresh(); err != nil {
			return err
		}
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(writeTimeout))
	})
}

// wsMessage 是通道上的报文。两个方向都用同一个结构，靠 type 区分。
type wsMessage struct {
	Type    string `json:"type"`
	Version int64  `json:"version"`
}

// isWebSocketUpgrade 判断这次请求是不是 WS 升级请求。
//
// 升级请求必须在握手前完成签名校验：升级成功后连接已被接管，
// GoFrame 的中间件与控制器逻辑都不再生效。
func isWebSocketUpgrade(r *http.Request) bool {
	return websocket.IsWebSocketUpgrade(r)
}
