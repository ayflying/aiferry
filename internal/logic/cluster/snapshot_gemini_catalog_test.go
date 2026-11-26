package cluster

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSnapshotGeminiCatalogRoundTripAndPrune(t *testing.T) {
	master, worker := openWorkerDatabase(t), openWorkerDatabase(t)
	ctx := context.Background()
	svc := &Service{database: master}
	baseline, err := svc.Fingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = master.Exec(ctx, `INSERT INTO gemini_credential_models(channel_id,channel_credential_id,upstream_name,metadata) VALUES (10,11,'gemini-a','{}'), (10,12,'gemini-b','{}')`); err != nil {
		t.Fatal(err)
	}
	changed, err := svc.Fingerprint(ctx)
	if err != nil || changed == baseline {
		t.Fatalf("fingerprint missed the account catalog update: %v", err)
	}
	snapshot, err := buildSnapshot(ctx, master, 1)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var received Snapshot
	if err = json.Unmarshal(wire, &received); err != nil {
		t.Fatal(err)
	}
	if err = applySnapshot(ctx, worker, &received); err != nil {
		t.Fatal(err)
	}
	if countRows(t, worker, "gemini_credential_models") != 2 {
		t.Fatal("mirroring lost account-specific catalog entries")
	}
	received.GeminiCredentialModels = received.GeminiCredentialModels[1:]
	if err = applySnapshot(ctx, worker, &received); err != nil {
		t.Fatal(err)
	}
	id, err := worker.GetValue(ctx, `SELECT channel_credential_id FROM gemini_credential_models`)
	if err != nil || id.Uint64() != 12 || countRows(t, worker, "gemini_credential_models") != 1 {
		t.Fatalf("mirroring did not remove only the revoked grant: %v, %v", id, err)
	}
	// An older master snapshot with no account directory is accepted, but
	// absence is never interpreted as a grant on a newer worker.
	received.GeminiCredentialModels = nil
	if err = applySnapshot(ctx, worker, &received); err != nil || countRows(t, worker, "gemini_credential_models") != 0 {
		t.Fatalf("missing account directory is not fail-closed: %v", err)
	}
}
