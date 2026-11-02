package cluster

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
)

func TestSnapshotModelMetadataRoundTripAndPrune(t *testing.T) {
	master := openWorkerDatabase(t)
	worker := openWorkerDatabase(t)
	ctx := context.Background()
	body := `{"display_name":null,"context_length":null,"input_modalities":[],"output_modalities":null,"capabilities":{"reasoning":false,"structured_output":null}}`
	for _, row := range []struct {
		id   int
		name string
		body string
	}{{0, "public-model", body}, {11, "upstream-model", `{"context_length":8192,"reasoning":true}`}, {12, "NULL-json", "null"}} {
		if _, err := master.Exec(ctx, "INSERT INTO model_metadata(channel_id,model_name,metadata) VALUES(?,?,?)", row.id, row.name, row.body); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{database: master}
	before, err := service.Fingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := buildSnapshot(ctx, master, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ModelMetadata) != 3 {
		t.Fatalf("metadata rows: %d", len(snapshot.ModelMetadata))
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var received Snapshot
	if err = json.Unmarshal(data, &received); err != nil {
		t.Fatal(err)
	}
	if err = applySnapshot(ctx, worker, &received); err != nil {
		t.Fatal(err)
	}
	// 与 /models 的 modelmetadata.Load 相同查询条件读取，验证协议未双重编码或丢弃 null。
	var stored []struct {
		ChannelId uint64 `orm:"channel_id"`
		ModelName string `orm:"model_name"`
		Metadata  string `orm:"metadata"`
	}
	if err = worker.Model("model_metadata").Ctx(ctx).Where("channel_id", 0).WhereIn("model_name", []string{"public-model"}).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Metadata != body {
		t.Fatalf("stored JSON changed: %+v", stored)
	}
	var metadata modelmetadata.Metadata
	if err = json.Unmarshal([]byte(stored[0].Metadata), &metadata); err != nil {
		t.Fatal(err)
	}
	if err = modelmetadata.Validate(metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.ContextLength != nil || metadata.InputModalities == nil || len(metadata.InputModalities) != 0 || metadata.OutputModalities != nil || metadata.Reasoning == nil || *metadata.Reasoning {
		t.Fatalf("metadata null/false/empty semantics changed: %+v", metadata)
	}
	nullJSON, err := worker.GetValue(ctx, "SELECT metadata FROM model_metadata WHERE channel_id=12")
	if err != nil || nullJSON.String() != "null" {
		t.Fatalf("JSON null lost: %v %v", nullJSON, err)
	}
	if _, err = master.Exec(ctx, "UPDATE model_metadata SET metadata=? WHERE channel_id=0", `{"reasoning":true}`); err != nil {
		t.Fatal(err)
	}
	after, err := service.Fingerprint(ctx)
	if err != nil || before == after {
		t.Fatalf("metadata fingerprint unchanged: %v", err)
	}
	received.ModelMetadata = nil
	if err = applySnapshot(ctx, worker, &received); err != nil {
		t.Fatal(err)
	}
	if countRows(t, worker, "model_metadata") != 0 {
		t.Fatal("deleted metadata retained")
	}
}
