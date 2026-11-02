package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func newLedgerWorker(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	secret := strings.Repeat("ledger-secret-", 3)
	verifier := NewSigner(secret, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, err := verifier.Verify(r.Context(), r, body); err != nil {
			t.Errorf("verify complete signed request URI %s: %v", r.URL.RequestURI(), err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	s := &Service{
		config:   config.App{Cluster: config.Cluster{Role: config.RoleWorker, NodeName: "ledger-worker", MasterURL: server.URL, Secret: secret, BatchSize: 2}},
		database: openWorkerDatabase(t), signer: NewSigner(secret, nil), client: server.Client(),
	}
	if err := s.applyAndPersist(context.Background(), sampleSnapshot(5)); err != nil {
		t.Fatal(err)
	}
	return s
}

func insertLedgerUsage(t *testing.T, s *Service, count int) {
	t.Helper()
	for i := range count {
		if _, err := s.database.Exec(context.Background(), "INSERT INTO usage_logs(request_id,user_id,api_key_id,estimated_cost) VALUES(?,7,21,0.25)", fmt.Sprintf("ledger-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
}

func assertLedgerBalance(t *testing.T, s *Service, want float64) {
	t.Helper()
	balance, err := s.database.GetValue(context.Background(), "SELECT balance FROM users WHERE id=7")
	if err != nil || balance.Float64() != want {
		t.Fatalf("balance=%v err=%v, want %v", balance, err, want)
	}
}

func TestLedgerSyncFlushFailurePreservesLocalSnapshot(t *testing.T) {
	var configs atomic.Int32
	s := newLedgerWorker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workerPathPrefix+"/usage" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		configs.Add(1)
		writeGzip(t, w, sampleSnapshot(6))
	})
	insertLedgerUsage(t, s, 3)
	if _, err := s.database.Exec(context.Background(), "UPDATE users SET balance=-0.75 WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncOnce(context.Background()); err == nil {
		t.Fatal("flush rejection must stop SyncOnce")
	}
	if configs.Load() != 0 || s.pendingRows(context.Background()) != 3 {
		t.Fatalf("failed flush fetched snapshot or lost rows: configs=%d pending=%d", configs.Load(), s.pendingRows(context.Background()))
	}
	assertLedgerBalance(t, s, -0.75)
	if version, err := s.localConfigVersion(context.Background()); err != nil || version != 5 {
		t.Fatalf("failed sync changed version=%d err=%v", version, err)
	}
}

func TestLedgerSyncPendingCountFailurePreservesLocalSnapshot(t *testing.T) {
	var requests atomic.Int32
	s := newLedgerWorker(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeGzip(t, w, sampleSnapshot(6))
	})
	ctx := context.Background()
	if _, err := s.database.Exec(ctx, "UPDATE users SET balance=-0.75 WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	// 只破坏临时 SQLite 的待回传表，模拟 COUNT 查询失败；不能当作空队列。
	if _, err := s.database.Exec(ctx, "DROP TABLE usage_logs"); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncOnce(ctx); err == nil {
		t.Fatal("pending count failure must stop SyncOnce")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("pending count failure made %d HTTP requests, want 0", got)
	}
	assertLedgerBalance(t, s, -0.75)
	if version, err := s.localConfigVersion(ctx); err != nil || version != 5 {
		t.Fatalf("failed pending count changed version=%d err=%v", version, err)
	}
}

func TestLedgerSyncDrainsEveryBatchBeforeSignedRefresh(t *testing.T) {
	var events []string
	s := newLedgerWorker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case workerPathPrefix + "/usage":
			var batch usageBatch
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Errorf("usage JSON: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			events = append(events, fmt.Sprintf("usage:%d", len(batch.Rows)))
			writeGzip(t, w, UsageAck{Accepted: len(batch.Rows)})
		case workerPathPrefix + "/config":
			events = append(events, "snapshot")
			if r.URL.Query().Get("refresh") != "1" || r.URL.Query().Get("version") != "5" {
				t.Errorf("must force same-version balance refresh: %s", r.URL.RequestURI())
			}
			// Changing only refresh must invalidate the signature (not merely sign Path).
			tampered := r.Clone(r.Context())
			query := tampered.URL.Query()
			query.Set("refresh", "0")
			tampered.URL.RawQuery = query.Encode()
			if _, err := NewSigner(strings.Repeat("ledger-secret-", 3), nil).Verify(r.Context(), tampered, nil); err == nil {
				t.Error("refresh query was not protected by signature")
			}
			snapshot := sampleSnapshot(5)
			snapshot.Users[0].Balance = 98.75
			writeGzip(t, w, snapshot)
		default:
			t.Errorf("unexpected URI %s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	})
	insertLedgerUsage(t, s, 5)
	if err := s.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"usage:2", "usage:2", "usage:1", "snapshot"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("request order=%v, want %v", events, want)
	}
	if pending := s.pendingRows(context.Background()); pending != 0 {
		t.Fatalf("pending=%d after sync", pending)
	}
	assertLedgerBalance(t, s, 98.75)
}

func TestLedgerSyncWaitsForCompletedLocalEntry(t *testing.T) {
	var events []string
	s := newLedgerWorker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case workerPathPrefix + "/usage":
			events = append(events, "usage")
			writeGzip(t, w, UsageAck{Accepted: 1})
		case workerPathPrefix + "/config":
			events = append(events, "snapshot")
			writeGzip(t, w, sampleSnapshot(5))
		}
	})
	previous := activeService.Swap(s)
	t.Cleanup(func() { activeService.Store(previous) })
	entered, release := make(chan struct{}), make(chan struct{})
	localDone := make(chan error, 1)
	go func() {
		localDone <- WithWorkerLedger(context.Background(), func(ctx context.Context) error {
			close(entered)
			<-release
			_, err := s.database.Exec(ctx, "INSERT INTO usage_logs(request_id,user_id,api_key_id,estimated_cost) VALUES('completed',7,21,0.25)")
			return err
		})
	}()
	<-entered
	syncDone := make(chan error, 1)
	go func() { syncDone <- s.SyncOnce(context.Background()) }()
	select {
	case err := <-syncDone:
		close(release)
		<-localDone
		t.Fatalf("snapshot crossed incomplete ledger entry: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-localDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-syncDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SyncOnce did not release ledger mutex")
	}
	if !reflect.DeepEqual(events, []string{"usage", "snapshot"}) {
		t.Fatalf("completed entry not flushed before snapshot: %v", events)
	}
}

func TestLedgerUsageWireOmitsRawNullTimes(t *testing.T) {
	rows := usageRows([]entity.UsageLogs{{Id: 1, RequestId: "zero-time", UserId: 7, ApiKeyId: 21}})
	if len(rows) != 1 {
		t.Fatalf("rows=%v", rows)
	}
	for key, value := range rows[0] {
		if _, raw := value.(gdb.Raw); raw {
			t.Fatalf("wire field %s contains SQL expression %v", key, value)
		}
	}
	for _, key := range []string{"id", "node_id", "created_at"} {
		if value, exists := rows[0][key]; exists {
			t.Fatalf("reserved/zero-time field %s must be omitted, got %#v", key, value)
		}
	}
	payload, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(payload), `"NULL"`) {
		t.Fatalf("invalid wire payload=%s err=%v", payload, err)
	}
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	populated := usageRows([]entity.UsageLogs{{RequestId: "timed", CreatedAt: stamp}})
	if _, exists := populated[0]["created_at"]; !exists {
		t.Fatal("nonzero creation time must survive wire conversion")
	}
}

func TestLedgerUsageRetryKeepsStableBatchID(t *testing.T) {
	var batches []usageBatch
	s := newLedgerWorker(t, func(w http.ResponseWriter, r *http.Request) {
		var batch usageBatch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("decode usage: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		batches = append(batches, batch)
		if len(batches) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeGzip(t, w, UsageAck{Accepted: len(batch.Rows), Replayed: true})
	})
	insertLedgerUsage(t, s, 2)
	if err := s.FlushUsage(context.Background()); err == nil {
		t.Fatal("first flush must fail")
	}
	if pending := s.pendingRows(context.Background()); pending != 2 {
		t.Fatalf("failed attempt deleted %d pending rows", pending)
	}
	if err := s.FlushUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || batches[0].BatchID == "" || !reflect.DeepEqual(batches[0], batches[1]) {
		t.Fatalf("retry changed batch: %+v", batches)
	}
	if pending := s.pendingRows(context.Background()); pending != 0 {
		t.Fatalf("successful retry left %d rows", pending)
	}
	original := []entity.UsageLogs{{Id: 1, RequestId: "one"}, {Id: 2, RequestId: "two"}}
	copyOfRows := append([]entity.UsageLogs(nil), original...)
	if stableBatchID(original) != stableBatchID(copyOfRows) {
		t.Fatal("batch identity depends on in-memory instance")
	}
	copyOfRows[1].RequestId = "replacement"
	if stableBatchID(original) == stableBatchID(copyOfRows) || stableBatchID(original) == stableBatchID(original[:1]) {
		t.Fatal("different row identities must produce distinct batches")
	}
}
