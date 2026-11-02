package cluster

import (
	"context"
	"testing"
)

func TestFingerprintDetectsSameCountRelationReplacement(t *testing.T) {
	db := openWorkerDatabase(t)
	ctx := context.Background()
	s := &Service{database: db}
	first, err := s.Fingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"channel_group_members", "api_key_models", "user_channel_groups"} {
		var insert, replace string
		switch table {
		case "channel_group_members":
			insert = "INSERT INTO channel_group_members(channel_group_id,channel_id) VALUES (1,2)"
			replace = "UPDATE channel_group_members SET channel_id=3"
		case "api_key_models":
			insert = "INSERT INTO api_key_models(api_key_id,model_name) VALUES (1,'a')"
			replace = "UPDATE api_key_models SET model_name='b'"
		case "user_channel_groups":
			insert = "INSERT INTO user_channel_groups(user_id,channel_group_id) VALUES (1,2)"
			replace = "UPDATE user_channel_groups SET channel_group_id=3"
		}
		if _, err = db.Exec(ctx, insert); err != nil {
			t.Fatal(err)
		}
		before, err := s.Fingerprint(ctx)
		if err != nil || before == first {
			t.Fatalf("insert undetected in %s: %v", table, err)
		}
		if _, err = db.Exec(ctx, replace); err != nil {
			t.Fatal(err)
		}
		after, err := s.Fingerprint(ctx)
		if err != nil || before == after {
			t.Fatalf("same-count replacement undetected in %s: %v", table, err)
		}
		again, err := s.Fingerprint(ctx)
		if err != nil || after != again {
			t.Fatalf("unstable hash: %v", err)
		}
	}
}
