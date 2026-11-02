package cluster

import (
	"context"
	"encoding/json"
	"testing"
)

func floatPointer(value float64) *float64 { return &value }

func TestSnapshotPreservesNullAndZeroLimitsAndPrices(t *testing.T) {
	master := openWorkerDatabase(t)
	worker := openWorkerDatabase(t)
	ctx := context.Background()
	for _, statement := range []string{
		"INSERT INTO api_keys(id,spend_limit,daily_spend_limit) VALUES (1,NULL,NULL),(2,0,0),(3,12.5,2.5)",
		"INSERT INTO model_prices(public_name,input_price,output_price,request_price) VALUES ('unknown',NULL,NULL,NULL),('free',0,0,0)",
		"INSERT INTO channel_models(id,channel_id,public_name,input_price,cached_input_price,output_price) VALUES (1,1,'unknown',NULL,NULL,NULL),(2,1,'free',0,0,0)",
	} {
		if _, err := master.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := buildSnapshot(ctx, master, 1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ApiKeys[0].DailySpendLimit != nil || snapshot.ApiKeys[1].DailySpendLimit == nil || *snapshot.ApiKeys[1].DailySpendLimit != 0 {
		t.Fatalf("master nullable scan lost limits: %+v", snapshot.ApiKeys)
	}
	// 实际协议 JSON 往返，不允许借助同进程指针绕过序列化验证。
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var received Snapshot
	if err = json.Unmarshal(payload, &received); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if err = applySnapshot(ctx, worker, &received); err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{
			"SELECT COUNT(*) FROM api_keys WHERE id=1 AND spend_limit IS NULL AND daily_spend_limit IS NULL",
			"SELECT COUNT(*) FROM api_keys WHERE id=2 AND spend_limit=0 AND daily_spend_limit=0",
			"SELECT COUNT(*) FROM api_keys WHERE id=3 AND spend_limit=12.5 AND daily_spend_limit=2.5",
			"SELECT COUNT(*) FROM model_prices WHERE public_name='unknown' AND input_price IS NULL AND output_price IS NULL AND request_price IS NULL",
			"SELECT COUNT(*) FROM model_prices WHERE public_name='free' AND input_price=0 AND output_price=0 AND request_price=0",
			"SELECT COUNT(*) FROM channel_models WHERE id=1 AND input_price IS NULL AND cached_input_price IS NULL AND output_price IS NULL",
			"SELECT COUNT(*) FROM channel_models WHERE id=2 AND input_price=0 AND cached_input_price=0 AND output_price=0",
		} {
			value, err := worker.GetValue(ctx, query)
			if err != nil || value.Int() != 1 {
				t.Fatalf("round %d null/zero semantics lost: %s: %v %v", round, query, value, err)
			}
		}
		// 第二轮验证 NULL 能覆盖旧值，不会被 ORM 当成忽略字段。
		if _, err = worker.Exec(ctx, "UPDATE api_keys SET daily_spend_limit=99 WHERE id=1"); err != nil {
			t.Fatal(err)
		}
	}
}
