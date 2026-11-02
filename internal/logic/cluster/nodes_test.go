package cluster

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yunloli/aiferry/internal/config"
)

func TestNodeRevocationSurvivesRegistration(t *testing.T) {
	db := openWorkerDatabase(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, `CREATE TABLE cluster_nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE, address TEXT, version TEXT, status INTEGER DEFAULT 1, last_heartbeat_at TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{database: db}
	node, err := s.RegisterNode(ctx, "worker-a", "http://worker:8080", "1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterNode(ctx, "worker-a", "http://new:8080", "2"); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteNode(ctx, node.Id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FindNodeByName(ctx, "worker-a"); err == nil {
		t.Fatal("revoked node authorized")
	}
	if _, err = s.RegisterNode(ctx, "worker-a", "http://evil:8080", "3"); err == nil {
		t.Fatal("revoked node registered again")
	}
	value, err := db.GetValue(ctx, "SELECT status FROM cluster_nodes WHERE name = ?", "worker-a")
	if err != nil || value.IsNil() || value.Int() != 0 {
		t.Fatalf("missing tombstone: %v %v", value, err)
	}
	address, _ := db.GetValue(ctx, "SELECT address FROM cluster_nodes WHERE name = ?", "worker-a")
	if address.String() != "http://new:8080" {
		t.Fatalf("revoked metadata changed: %s", address)
	}
}

func TestAuthorizeMasterDoesNotRequireNodeTable(t *testing.T) {
	secret := strings.Repeat("s", 32)
	s := &Service{config: config.App{Cluster: config.Cluster{Role: config.RoleWorker, Secret: secret, MasterNodeName: "control-a"}}, signer: NewSigner(secret, nil)}
	for _, name := range []string{"worker-a", "master", "control-a"} {
		r := httptest.NewRequest("POST", "http://worker/internal/cluster/notify", nil)
		headers, err := s.signer.Headers(name, r.Method, r.URL.RequestURI(), nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header = headers
		err = s.AuthorizeMaster(context.Background(), r, nil)
		if (err == nil) != (name == "control-a") {
			t.Fatalf("identity %s: %v", name, err)
		}
	}
}
