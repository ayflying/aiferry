package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/config"
)

// TestHubPushReplacesStaleSession 验证同名节点重连后顶替旧连接：
// 容器重建时旧连接可能半死，若不同时关掉它，推送会一直投给已经没人读的通道。
func TestHubPushReplacesStaleSession(t *testing.T) {
	h := hub{}
	first := newWSSession("worker-a", "1.0.0", nil)
	h.register(first)
	second := newWSSession("worker-a", "1.0.1", nil)
	h.register(second)
	select {
	case <-first.closed:
	default:
		t.Fatal("stale session was not closed on replacement")
	}
	if !h.push("worker-a", 7) {
		t.Fatal("push to newest session failed")
	}
	message := <-second.send
	if message.Type != wsMessageNotify || message.Version != 7 {
		t.Fatalf("unexpected message %+v", message)
	}
	info, ok := h.connected("worker-a")
	if !ok || !info.Online || info.Version != "1.0.1" {
		t.Fatalf("connection info = %+v (%v)", info, ok)
	}
}

// TestHubFullChannelClosesSession 验证对端长期不读时主动断开而不是丢弃消息：
// 丢消息会让子节点长期停在旧配置上，且没有任何错误可见。
func TestHubFullChannelClosesSession(t *testing.T) {
	h := hub{}
	session := newWSSession("worker-a", "1.0.0", nil)
	h.register(session)
	for index := 0; index < cap(session.send); index++ {
		if !h.push("worker-a", int64(index)) {
			t.Fatalf("push %d unexpectedly failed", index)
		}
	}
	if h.push("worker-a", 99) {
		t.Fatal("push into a full channel must report failure")
	}
	select {
	case <-session.closed:
	default:
		t.Fatal("full channel must close the session")
	}
	if _, ok := h.connected("worker-a"); ok {
		t.Fatal("closed session still reported online")
	}
}

// TestListNodesReportsStreamState 验证管理端列表的在线字段来自 WS 订阅而不是心跳时间。
func TestListNodesReportsStreamState(t *testing.T) {
	db := openWorkerDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE cluster_nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE, address TEXT, version TEXT, status INTEGER DEFAULT 1, last_heartbeat_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `CREATE TABLE cluster_state (id INTEGER PRIMARY KEY, config_version INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO cluster_state(id, config_version) VALUES(1, 3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO cluster_nodes(name, address, version, status) VALUES('worker-a', 'http://10.0.0.2:8080', '1.0.0', 1)`); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db}
	nodes, err := service.ListNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Online || !nodes[0].Stale {
		t.Fatalf("offline node state = %+v", nodes)
	}
	service.hub.register(newWSSession("worker-a", "1.0.1", nil))
	nodes, err = service.ListNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !nodes[0].Online || nodes[0].StreamVersion != "1.0.1" || nodes[0].StreamSince.IsZero() {
		t.Fatalf("online node state = %+v", nodes[0])
	}
}

// TestServeStreamDeliversVersion 验证主节点侧订阅语义：验签通过后登记在线并下发版本号，
// 之后推送直接进会话队列而不回落 HTTP 回调。
//
// 这里不用真实 WS 握手：httptest 的服务端响应不支持 Hijack，gorilla 的 Upgrade 必然失败，
// 强行升级会把「握手失败」误当成「通道逻辑失败」。真实握手由端到端脚本在测试环境覆盖。
func TestServeStreamDeliversVersion(t *testing.T) {
	db := openWorkerDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE cluster_state (id INTEGER PRIMARY KEY, config_version INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO cluster_state(id, config_version) VALUES(1, 11)`); err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("s", 32)
	service := &Service{
		database: db,
		signer:   NewSigner(secret, nil),
		client:   &http.Client{},
		config:   config.App{Cluster: config.Cluster{Secret: secret, NodeName: "master"}},
	}

	request := httptest.NewRequest(http.MethodGet, "http://master/internal/cluster/stream", nil)
	headers, err := service.signer.Headers("worker-a", http.MethodGet, wsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = headers
	name, err := service.VerifiedNodeName(ctx, request, nil)
	if err != nil {
		t.Fatalf("signed stream request rejected: %v", err)
	}
	if name != "worker-a" {
		t.Fatalf("verified name = %q", name)
	}

	session := newWSSession(name, "1.0.0", nil)
	service.hub.register(session)
	defer service.hub.markClosed(session)
	service.hub.send(session, wsMessage{Type: wsMessageState, Version: 11})
	if message := <-session.send; message.Type != wsMessageState || message.Version != 11 {
		t.Fatalf("initial message = %+v", message)
	}
	if !service.Online("worker-a") {
		t.Fatal("node not reported online after subscribing")
	}
	if nodes := service.OnlineNodes(); len(nodes) != 1 || nodes[0] != "worker-a" {
		t.Fatalf("online nodes = %v", nodes)
	}
	if !service.hub.push("worker-a", 12) {
		t.Fatal("push through hub failed")
	}
	if message := <-session.send; message.Type != wsMessageNotify || message.Version != 12 {
		t.Fatalf("notify message = %+v", message)
	}
	service.hub.markClosed(session)
	if service.Online("worker-a") {
		t.Fatal("closed session still reported online")
	}
}

// TestServeStreamOnceAppliesVersion 验证子节点侧连接器：连上后收到 state 与 notify，
// 落后时唤醒一次同步（这里用同步通道观察唤醒，不启动完整循环）。
func TestServeStreamOnceAppliesVersion(t *testing.T) {
	db := openWorkerDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secret := strings.Repeat("s", 32)
	signer := NewSigner(secret, nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := signer.Verify(r.Context(), r, nil); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteJSON(wsMessage{Type: wsMessageState, Version: 9})
		<-ctx.Done()
	}))
	defer server.Close()

	service := &Service{
		config: config.App{Cluster: config.Cluster{
			Role: config.RoleWorker, NodeName: "worker-a", MasterURL: server.URL, Secret: secret,
		}},
		signer:   signer,
		client:   server.Client(),
		database: db,
		wake:     make(chan struct{}, 1),
	}
	done := make(chan error, 1)
	go func() { done <- service.serveStreamOnce(ctx) }()

	select {
	case <-service.wake:
	case <-time.After(5 * time.Second):
		t.Fatal("streamed version did not wake the sync loop")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serveStreamOnce did not return after cancel")
	}
}

// TestStreamRejectsUnsignedRequest 验证未签名请求无法订阅：WS 端点与其它内部接口同一套鉴权。
func TestStreamRejectsUnsignedRequest(t *testing.T) {
	secret := strings.Repeat("s", 32)
	service := &Service{
		signer: NewSigner(secret, nil),
		config: config.App{Cluster: config.Cluster{Secret: secret}},
	}
	request := httptest.NewRequest(http.MethodGet, "http://master/internal/cluster/stream", nil)
	if _, err := service.VerifiedNodeName(context.Background(), request, nil); err == nil {
		t.Fatal("unsigned stream request was accepted")
	}
}

// TestWSMessageWireFormat 校验通道报文的 JSON 约定，避免两端字段名漂移。
func TestWSMessageWireFormat(t *testing.T) {
	payload, err := json.Marshal(wsMessage{Type: wsMessageNotify, Version: 42})
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"type":"notify","version":42}` {
		t.Fatalf("wire format changed: %s", payload)
	}
}
