package cluster

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// upgrader 用于把已验签的 HTTP 请求升级成 WS。
//
// 这里不做来源校验：连接方是持有集群密钥的子节点（签名在升级前已校验），
// 不是浏览器，正常请求不带 Origin 头；设置固定的允许来源反而会误伤合法节点。
// 真正的访问控制由签名 + 节点注册状态承担。
var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	ReadBufferSize:   1 << 10,
	WriteBufferSize:  1 << 10,
}

// wsSession 是某个子节点当前持有的一条连接。
//
// 同名节点只保留最新一条：容器重建或网络抖动会留下旧连接的半死状态，
// 新订阅顶替旧订阅才能保证「在线」判定跟随真实链路。
type wsSession struct {
	name string
	// version 是子节点自报的应用版本，仅用于日志排查。
	version string
	// connectedAt 是本次订阅建立时间，管理端据此显示「已连接时长」。
	connectedAt time.Time
	conn        *websocket.Conn
	send        chan wsMessage
	// closed 保证 close 只执行一次，避免重复关闭已关闭的 channel 造成 panic。
	once   sync.Once
	closed chan struct{}
}

func newWSSession(name, version string, conn *websocket.Conn) *wsSession {
	return &wsSession{
		name:        name,
		version:     version,
		connectedAt: time.Now(),
		conn:        conn,
		send:        make(chan wsMessage, 8),
		closed:      make(chan struct{}),
	}
}

// push 投递一条消息。通道满说明对端长期不读，此时把会话从在线表摘除并关闭连接：
// 丢掉最新版本号会让子节点长期停在旧配置上，而「在线」又会让推送一直被吞掉。
// 断开后子节点会重连，在线状态与推送路径都能自愈。
func (h *hub) send(w *wsSession, message wsMessage) bool {
	select {
	case <-w.closed:
		return false
	default:
	}
	select {
	case w.send <- message:
		return true
	default:
		h.markClosed(w)
		return false
	}
}

func (w *wsSession) close() {
	w.once.Do(func() {
		close(w.closed)
	})
}

// markClosed 在关闭连接的同时把它从在线表摘除。
//
// 只 close 通道而不删表会让「在线」长期为真，推送全部被吞掉，
// 子节点也只能等自己发现断链才重连，期间配置更新无法感知。
func (h *hub) markClosed(session *wsSession) {
	session.close()
	h.forget(session)
}

// writeLoop 独占写协程：gorilla/websocket 不允许并发写。
//
// ctx 只用于「服务停止」，不能传请求上下文：请求上下文在 HTTP 处理返回时即被取消，
// 而连接这时应当继续存活并发心跳。
func (h *hub) writeLoop(ctx context.Context, w *wsSession) {
	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			h.markClosed(w)
			return
		case <-w.closed:
			_ = w.conn.Close()
			return
		case message := <-w.send:
			_ = w.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := w.conn.WriteJSON(message); err != nil {
				h.markClosed(w)
			}
		case <-ping.C:
			_ = w.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := w.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.markClosed(w)
			}
		}
	}
}

// readLoop 只负责「感知断开」与刷新读期限。子节点不通过这条连接发指令，
// 因此收到的数据帧一律忽略，仅把 pong 当作链路健康证据。
func (h *hub) readLoop(w *wsSession) {
	defer h.markClosed(w)
	w.conn.SetReadLimit(wsReadLimit)
	_ = w.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	w.conn.SetPongHandler(func(string) error {
		return w.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		if _, _, err := w.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// ConnectionInfo 是管理端展示用的在线信息。
type ConnectionInfo struct {
	// Online 表示当前有一条活跃 WS 连接。
	Online bool `json:"online"`
	// Since 是本次订阅建立时间；不在线时为零值。
	Since time.Time `json:"since"`
	// Version 是子节点自报的应用版本，只在内存里，不落库。
	Version string `json:"version"`
}

// hub 维护节点名到连接的映射，是「在线状态」的唯一真相来源。
//
// 在线判定刻意不查数据库：WS 断开是唯一可靠的离线证据，
// 心跳表中的时间戳只能说明「最近来过」，无法区分进程僵死。
type hub struct {
	mu       sync.RWMutex
	sessions map[string]*wsSession
}

// register 顶替同名旧连接并返回新会话。
//
// 顶替而不是拒绝：子节点网络闪断后立即重连是常态，
// 拒绝会让它卡在重试退避里，反而更难恢复。
func (h *hub) register(session *wsSession) *wsSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions == nil {
		h.sessions = make(map[string]*wsSession, 8)
	}
	previous := h.sessions[session.name]
	h.sessions[session.name] = session
	if previous != nil && previous != session {
		previous.close()
	}
	return previous
}

// unregister 移除指定会话；同名新连接已顶替时不会被误删。
func (h *hub) unregister(session *wsSession) {
	h.forget(session)
}

// forget 无条件移除某节点的会话，用于「通道被判定失效」的场景。
func (h *hub) forget(session *wsSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current, ok := h.sessions[session.name]; ok && current == session {
		delete(h.sessions, session.name)
	}
}

// push 向指定节点投递版本通知，返回是否真的送达了当前连接。
func (h *hub) push(name string, version int64) bool {
	h.mu.RLock()
	session := h.sessions[name]
	h.mu.RUnlock()
	if session == nil {
		return false
	}
	return h.send(session, wsMessage{Type: wsMessageNotify, Version: version})
}

// connected 返回节点当前是否在线。
func (h *hub) connected(name string) (ConnectionInfo, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	session, ok := h.sessions[name]
	if !ok {
		return ConnectionInfo{}, false
	}
	return ConnectionInfo{Online: true, Since: session.connectedAt, Version: session.version}, true
}

// snapshot 返回全部在线节点名，供管理端判定与排障使用。
func (h *hub) snapshot() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	names := make([]string, 0, len(h.sessions))
	for name := range h.sessions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Online 返回某节点当前是否通过 WS 订阅着主节点。
// 未启用集群或本节点是子节点时永远返回 false。
func (s *Service) Online(name string) bool {
	if s == nil || s.IsWorker() {
		return false
	}
	_, ok := s.hub.connected(name)
	return ok
}

// OnlineNodes 返回当前在线的节点名列表，用于排查「推送为什么没到」。
func (s *Service) OnlineNodes() []string {
	if s == nil || s.IsWorker() {
		return nil
	}
	return s.hub.snapshot()
}

// ServeStream 处理子节点的 WS 订阅。调用前必须已经完成签名校验：
// 这里的 name 来自验签结果，不接受请求体或查询参数里的自称。
//
// 握手成功后先下发一条 state 消息（携带当前配置版本），
// 让子节点立刻知道是否需要补一次同步，而不用等下一次轮询。
//
// 连接一旦被 Upgrade 接管，生命周期就不再受 HTTP 请求上下文控制：
// GoFrame 会在请求处理返回后取消该上下文，若写协程跟着它退出，
// 连接看起来正常却再也收不到 ping，子节点只能靠读超时反复重连
// （2026-11-06 线上实测表现为每 50 秒断开一次）。
func (s *Service) ServeStream(ctx context.Context, w http.ResponseWriter, r *http.Request, name, version string) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade 失败时已经自行写过响应，这里只能返回错误供上层记日志。
		return err
	}
	session := newWSSession(name, version, conn)
	if previous := s.hub.register(session); previous != nil {
		s.clusterLogger().Infof(ctx, "cluster: node %s reconnected over websocket, replaced stale session", name)
	}
	// loopCtx 只表达「服务是否停止」，不继承请求上下文。
	loopCtx := context.Background()
	go s.hub.writeLoop(loopCtx, session)
	go s.hub.readLoop(session)
	defer s.hub.markClosed(session)

	current, err := s.ConfigVersion(ctx)
	if err != nil {
		// 版本号读不到不影响订阅本身：通知仍会随配置变更下发。
		s.clusterLogger().Warningf(ctx, "cluster: read config version for stream: %v", err)
	}
	s.hub.send(session, wsMessage{Type: wsMessageState, Version: current})
	s.clusterLogger().Infof(ctx, "cluster: node %s subscribed to config stream (version %d)", name, current)
	<-session.closed
	s.clusterLogger().Infof(ctx, "cluster: node %s config stream closed", name)
	return nil
}
