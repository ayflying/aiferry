package relay

import (
	"testing"

	"github.com/yunloli/aiferry/internal/logic/apikey"
)

func TestRouteCacheKeyIncludesVersion(t *testing.T) {
	if routeCacheKey("gpt-4o", 0) != "aiferry:routes:gpt-4o:0" {
		t.Fatalf("version 0 key mismatch: %s", routeCacheKey("gpt-4o", 0))
	}
	if routeCacheKey("gpt-4o", 42) == routeCacheKey("gpt-4o", 43) {
		t.Fatal("different versions must produce different cache keys")
	}
}

func TestFilterCandidatesAppliesGroupPolicy(t *testing.T) {
	key := apikey.AuthKey{
		UserRole:            "user",
		UserChannelGroupIDs: []uint64{2},
	}
	candidates := []Candidate{
		{ChannelID: 1, GroupIDs: nil},            // 公共渠道，任何人可用
		{ChannelID: 2, GroupIDs: []uint64{2}},    // 用户所在分组
		{ChannelID: 3, GroupIDs: []uint64{7}},    // 其他分组，应被过滤
		{ChannelID: 4, GroupIDs: []uint64{2, 7}}, // 命中其中一个分组
	}
	filtered := filterCandidates(candidates, key, []string{"admin"})
	ids := make(map[uint64]bool, len(filtered))
	for _, candidate := range filtered {
		ids[candidate.ChannelID] = true
	}
	if len(filtered) != 3 || !ids[1] || !ids[2] || !ids[4] || ids[3] {
		t.Fatalf("unexpected filtered set: %+v", filtered)
	}
}

func TestFilterCandidatesAdminSeesAll(t *testing.T) {
	key := apikey.AuthKey{
		UserRole:            "admin",
		UserChannelGroupIDs: nil,
	}
	candidates := []Candidate{
		{ChannelID: 1, GroupIDs: []uint64{7}},
		{ChannelID: 2, GroupIDs: []uint64{9}},
	}
	filtered := filterCandidates(candidates, key, []string{"admin"})
	if len(filtered) != 2 {
		t.Fatalf("admin should see all candidates, got %+v", filtered)
	}
}

func TestFilterCandidatesDoesNotMutateInput(t *testing.T) {
	key := apikey.AuthKey{UserRole: "user", UserChannelGroupIDs: nil}
	candidates := []Candidate{
		{ChannelID: 1, GroupIDs: []uint64{7}},
		{ChannelID: 2, GroupIDs: nil},
	}
	_ = filterCandidates(candidates, key, []string{"admin"})
	if len(candidates) != 2 {
		t.Fatalf("input slice must stay untouched, got %d entries", len(candidates))
	}
}

func TestFilterCandidatesHonorsConfiguredAdminRoles(t *testing.T) {
	key := apikey.AuthKey{UserRole: "superuser"}
	candidates := []Candidate{{ChannelID: 1, GroupIDs: []uint64{7}}}
	if got := filterCandidates(candidates, key, []string{"superuser"}); len(got) != 1 {
		t.Fatalf("configured admin role should pass, got %+v", got)
	}
	if got := filterCandidates(candidates, key, []string{"admin"}); len(got) != 0 {
		t.Fatalf("non-admin role should be filtered, got %+v", got)
	}
}
