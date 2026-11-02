package cluster

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"
)

func TestConfigPayloadRefreshesUnchangedVersion(t *testing.T) {
	db := openWorkerDatabase(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, "CREATE TABLE cluster_state(id INTEGER PRIMARY KEY, config_version INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO cluster_state VALUES(1,5)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO users(id,balance) VALUES(7,123.5)"); err != nil {
		t.Fatal(err)
	}
	s := &Service{database: db}
	if _, unchanged, err := s.ConfigPayload(ctx, 5); err != nil || !unchanged {
		t.Fatalf("conditional: %v %v", unchanged, err)
	}
	payload, unchanged, err := s.ConfigPayload(ctx, 5, true)
	if err != nil || unchanged {
		t.Fatalf("refresh: %v %v", unchanged, err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var snapshot Snapshot
	if err = json.NewDecoder(reader).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != 5 || len(snapshot.Users) != 1 || snapshot.Users[0].Balance != 123.5 {
		t.Fatalf("refresh did not carry current account: %+v", snapshot.Users)
	}
}
