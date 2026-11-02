package cluster

import (
	"context"
	"testing"
)

// 所有 worker 非本地表必须进入快照和指纹集合，新增配置表时避免只补建表而漏同步。
func TestWorkerSchemaSnapshotFingerprintCoverage(t *testing.T) {
	db := openWorkerDatabase(t)
	rows, err := db.GetAll(context.Background(), "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatal(err)
	}
	local := map[string]bool{"usage_logs": true, "local_channel_state": true, "local_credential_state": true, "local_combo_state": true, "local_events": true, "sync_state": true}
	mirror := map[string]bool{}
	for _, spec := range snapshotTables {
		if mirror[spec.name] {
			t.Fatalf("duplicate snapshot table %s", spec.name)
		}
		mirror[spec.name] = true
	}
	fingerprints := map[string]bool{}
	for _, name := range fingerprintTables {
		fingerprints[name] = true
	}
	schema := map[string]bool{}
	for _, row := range rows {
		name := row["name"].String()
		schema[name] = true
		if !local[name] && !mirror[name] {
			t.Errorf("worker configuration table %s missing from snapshot", name)
		}
	}
	for name := range mirror {
		if !schema[name] {
			t.Errorf("snapshot table %s missing worker schema", name)
		}
		if !fingerprints[name] {
			t.Errorf("snapshot table %s missing fingerprint", name)
		}
	}
	for name := range fingerprints {
		if !mirror[name] {
			t.Errorf("fingerprint table %s missing snapshot", name)
		}
	}
	// 从数据库实际构建一次也能验证 loaders 不遗漏空表的读取约定。
	if _, err = buildSnapshot(context.Background(), db, 1); err != nil {
		t.Fatal(err)
	}
}
