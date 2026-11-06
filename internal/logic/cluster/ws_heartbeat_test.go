package cluster

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestConfigureStreamReaderHeartbeat 只交换控制帧：主节点发 ping，子节点持续
// ReadMessage 自动回 pong。没有业务帧或子节点主动 ping 可以意外续期。
func TestConfigureStreamReaderHeartbeat(t *testing.T) {
	const (
		wait         = 500 * time.Millisecond
		writeTimeout = 250 * time.Millisecond
		interval     = 50 * time.Millisecond
	)
	for _, legacy := range []bool{false, true} {
		name := "ping_refreshes_worker_deadline"
		if legacy {
			name = "legacy_pong_only_times_out_despite_replying"
		}
		t.Run(name, func(t *testing.T) {
			master, worker := heartbeatSocketPair(t)
			configureStreamReader(master, wait, writeTimeout)
			if legacy {
				// 修复前配置：默认 PingHandler 会回 pong，却不会刷新子端读期限。
				// 保留真实的默认处理器，不能在测试中用手工 pong 掩盖回归。
				worker.SetReadLimit(wsReadLimit)
				if err := worker.SetReadDeadline(time.Now().Add(wait)); err != nil {
					t.Fatal(err)
				}
				worker.SetPongHandler(func(string) error {
					return worker.SetReadDeadline(time.Now().Add(wait))
				})
			} else {
				configureStreamReader(worker, wait, writeTimeout)
			}

			// 包装而不替换生产 PongHandler 的续期行为，同时确认 pong 原样回显。
			pongs := make(chan string, 64)
			onPong := master.PongHandler()
			master.SetPongHandler(func(data string) error {
				if err := onPong(data); err != nil {
					return err
				}
				pongs <- data
				return nil
			})
			var (
				masterRead = startHeartbeatReader(t, master)
				workerRead = startHeartbeatReader(t, worker)
				ticker     = time.NewTicker(interval)
				alive      = time.NewTimer(3 * wait)
				started    = time.Now()
				pongCount  int
				lastPong   time.Duration
			)
			defer ticker.Stop()
			defer alive.Stop()
			for {
				select {
				case <-ticker.C:
					if err := master.WriteControl(websocket.PingMessage, []byte("heartbeat"), time.Now().Add(writeTimeout)); err != nil {
						t.Fatalf("send ping: %v", err)
					}
				case data := <-pongs:
					if data != "heartbeat" {
						t.Fatalf("pong payload = %q, want heartbeat", data)
					}
					pongCount++
					lastPong = time.Since(started)
				case <-masterRead.done:
					t.Fatalf("master stopped while worker should answer pings: %v", masterRead.err)
				case <-workerRead.done:
					if !legacy {
						t.Fatalf("healthy worker stopped after %v: %v", time.Since(started), workerRead.err)
					}
					requireHeartbeatTimeout(t, workerRead.err)
					if pongCount < 2 {
						t.Fatalf("legacy worker timed out without proving automatic pong replies: %d pongs", pongCount)
					}
					return // 超时后绝不再次读取同一个 gorilla 连接。
				case <-alive.C:
					if legacy {
						t.Fatal("legacy PongHandler-only worker unexpectedly survived three wait periods")
					}
					if pongCount < 3 || lastPong < 2*wait {
						t.Fatalf("heartbeat did not span multiple deadlines: %d pongs, last at %v", pongCount, lastPong)
					}
					// 健康连接经过多个 wait 后，关闭主端必须立即唤醒子端读取。
					if err := master.Close(); err != nil {
						t.Fatal(err)
					}
					awaitHeartbeatReader(t, workerRead, 2*wait)
					var timeout net.Error
					if errors.As(workerRead.err, &timeout) && timeout.Timeout() {
						t.Fatalf("worker noticed peer close only through deadline: %v", workerRead.err)
					}
					return
				}
			}
		})
	}
}

// TestConfigureStreamReaderMissingPong 验证对端仍在读 ping、但不返回 pong 时，
// 主节点不会被自己发出的 ping 续期，且读循环在期限内退出。
func TestConfigureStreamReaderMissingPong(t *testing.T) {
	const (
		wait         = 500 * time.Millisecond
		writeTimeout = 250 * time.Millisecond
	)
	master, worker := heartbeatSocketPair(t)
	configureStreamReader(master, wait, writeTimeout)
	pings := make(chan struct{}, 64)
	worker.SetPingHandler(func(string) error {
		pings <- struct{}{}
		return nil // 有意吞掉 ping，不回 pong。
	})
	var (
		masterRead = startHeartbeatReader(t, master)
		workerRead = startHeartbeatReader(t, worker)
		ticker     = time.NewTicker(50 * time.Millisecond)
		limit      = time.NewTimer(3 * wait)
		pingCount  int
	)
	defer ticker.Stop()
	defer limit.Stop()
	for {
		select {
		case <-ticker.C:
			if err := master.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)); err != nil {
				t.Fatalf("send ping: %v", err)
			}
		case <-pings:
			pingCount++
		case <-workerRead.done:
			t.Fatalf("worker should keep reading without a deadline: %v", workerRead.err)
		case <-masterRead.done:
			requireHeartbeatTimeout(t, masterRead.err)
			if pingCount < 2 {
				t.Fatalf("master timed out without proving peer received pings: %d", pingCount)
			}
			return
		case <-limit.C:
			t.Fatal("master did not stop after missing pong for three wait periods")
		}
	}
}

// heartbeatSocketPair 使用真实 TCP 监听和 gorilla Upgrade，不使用不能 Hijack
// 的 ResponseRecorder。每个测试独立连接、处理器和时限，不改动任何全局状态。
func heartbeatSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	type upgradeResult struct {
		conn *websocket.Conn
		err  error
	}
	upgraded := make(chan upgradeResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		upgraded <- upgradeResult{conn: conn, err: err}
	}))
	t.Cleanup(server.Close)
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	worker, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial heartbeat server: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	select {
	case result := <-upgraded:
		if result.err != nil {
			t.Fatalf("upgrade heartbeat connection: %v", result.err)
		}
		t.Cleanup(func() { _ = result.conn.Close() })
		return result.conn, worker
	case <-time.After(3 * time.Second):
		t.Fatal("server did not publish upgraded connection")
		return nil, nil
	}
}

type heartbeatReadResult struct {
	done chan struct{}
	err  error // 仅在 done 关闭后读取。
}

func startHeartbeatReader(t *testing.T, conn *websocket.Conn) *heartbeatReadResult {
	t.Helper()
	result := &heartbeatReadResult{done: make(chan struct{})}
	go func() {
		defer close(result.done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				result.err = err
				return // gorilla 读超时后连接不可恢复，不能继续 ReadMessage。
			}
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		awaitHeartbeatReader(t, result, 3*time.Second)
	})
	return result
}

func awaitHeartbeatReader(t *testing.T, result *heartbeatReadResult, budget time.Duration) {
	t.Helper()
	select {
	case <-result.done:
		if result.err == nil {
			t.Fatal("reader exited without an error")
		}
	case <-time.After(budget):
		t.Fatal("reader did not exit within cleanup/disconnect budget")
	}
}

func requireHeartbeatTimeout(t *testing.T, err error) {
	t.Helper()
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("read error = %v, want network timeout", err)
	}
}
