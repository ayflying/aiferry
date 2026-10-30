package cluster

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// TestWorkerSyncAndFlushRoundTrip 用假主节点验证子节点的完整链路：
// 签名注册 → 拉取快照并落镜像 → 版本未变走 304 → 回传用量后删除本地行。
func TestWorkerSyncAndFlushRoundTrip(t *testing.T) {
	database := openWorkerDatabase(t)
	store := newTestStore(t)
	secret := strings.Repeat("s", 32)
	signer := NewSigner(secret, store)

	var (
		registerCalls int
		configCalls   int
		flushedRows   int
	)
	snapshot := sampleSnapshot(5)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, err = signer.Verify(r.Context(), r, body); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/internal/cluster/register":
			registerCalls++
			writeGzip(t, w, registerResponse{NodeId: 9, ConfigVersion: 5})
		case "/internal/cluster/config":
			configCalls++
			if r.URL.Query().Get("version") == "5" {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			writeGzip(t, w, snapshot)
		case "/internal/cluster/usage":
			var batch usageBatch
			if err = json.Unmarshal(body, &batch); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			flushedRows += len(batch.Rows)
			writeGzip(t, w, UsageAck{Accepted: len(batch.Rows)})
		default:
			t.Logf("unexpected cluster path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := &Service{
		config: config.App{Cluster: config.Cluster{
			Role:          config.RoleWorker,
			NodeName:      "worker-a",
			MasterURL:     server.URL,
			Secret:        secret,
			BatchSize:     100,
			SyncInterval:  time.Minute,
			FlushInterval: 30 * time.Second,
		}},
		store:    store,
		signer:   signer,
		client:   server.Client(),
		database: database,
		wake:     make(chan struct{}, 1),
	}
	ctx := context.Background()

	if err := service.EnsureRegistered(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	if registerCalls != 1 {
		t.Fatalf("register calls = %d, want 1", registerCalls)
	}
	if nodeID, err := service.localInt(ctx, stateKeyNodeID); err != nil || nodeID != 9 {
		t.Fatalf("local node id = %d (%v), want 9", nodeID, err)
	}

	if err := service.SyncOnce(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if configCalls != 1 {
		t.Fatalf("config calls = %d, want 1", configCalls)
	}
	if version, err := service.localConfigVersion(ctx); err != nil || version != 5 {
		t.Fatalf("local config version = %d (%v), want 5", version, err)
	}
	var channels []entity.Channels
	if err := database.Model("channels").Ctx(ctx).Scan(&channels); err != nil {
		t.Fatalf("read channels: %v", err)
	}
	rawCount, err := database.GetCount(ctx, "SELECT COUNT(*) FROM channels")
	if err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if len(channels) != 1 || channels[0].Name != "主渠道" {
		t.Fatalf("mirror channels = %+v (raw count %d)", channels, rawCount)
	}

	// 版本已对齐：再次同步必须走 304，不重复传快照。
	if err := service.SyncOnce(ctx); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if configCalls != 2 {
		t.Fatalf("config calls = %d, want 2", configCalls)
	}

	for index := 1; index <= 2; index++ {
		requestID := "req-" + strconv.Itoa(index)
		if _, err := database.Exec(ctx,
			"INSERT INTO usage_logs(request_id, user_id, api_key_id, estimated_cost) VALUES(?, ?, ?, ?)",
			requestID, 7, 21, 0.25); err != nil {
			t.Fatalf("insert usage row: %v", err)
		}
	}
	if err := service.FlushUsage(ctx); err != nil {
		t.Fatalf("flush usage: %v", err)
	}
	if flushedRows != 2 {
		t.Fatalf("flushed rows = %d, want 2", flushedRows)
	}
	pending, err := database.GetCount(ctx, "SELECT COUNT(*) FROM usage_logs")
	if err != nil {
		t.Fatalf("count usage rows: %v", err)
	}
	if pending != 0 {
		t.Fatalf("local usage rows = %d after flush, want 0", pending)
	}
}

// writeGzip 按内部接口的响应约定写回 gzip JSON。
func writeGzip(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	payload, err := EncodeGzipJSON(value)
	if err != nil {
		t.Errorf("encode gzip: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	_, _ = w.Write(payload)
}
