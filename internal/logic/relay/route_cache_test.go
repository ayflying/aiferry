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

func TestFilterCandidatesAppliesCreatorAndGroupPolicy(t *testing.T) {
	key := apikey.AuthKey{
		UserId:              10,
		UserRole:            "user",
		UserChannelGroupIDs: []uint64{2},
	}
	candidates := []Candidate{
		{ChannelID: 1, CreatedByUserID: 10, GroupIDs: nil},         // 本人创建、未分组 → 可用
		{ChannelID: 2, CreatedByUserID: 99, GroupIDs: nil},         // 他人创建、未分组 → 拒绝
		{ChannelID: 3, CreatedByUserID: 99, GroupIDs: []uint64{2}}, // 用户所在分组 → 可用
		{ChannelID: 4, CreatedByUserID: 99, GroupIDs: []uint64{7}}, // 其他分组 → 拒绝
		{ChannelID: 5, CreatedByUserID: 10, GroupIDs: []uint64{7}}, // 本人创建且在其他分组 → 创建者可用
		{ChannelID: 6, CreatedByUserID: 99, GroupIDs: []uint64{2, 7}},
	}
	filtered := filterCandidates(candidates, key, []string{"admin"})
	ids := make(map[uint64]bool, len(filtered))
	for _, candidate := range filtered {
		ids[candidate.ChannelID] = true
	}
	if len(filtered) != 4 || !ids[1] || !ids[3] || !ids[5] || !ids[6] || ids[2] || ids[4] {
		t.Fatalf("unexpected filtered set: %+v", filtered)
	}
}

func TestFilterCandidatesAdminStillRestricted(t *testing.T) {
	// 管理员不再豁免：仅创建者或分组成员可用。
	key := apikey.AuthKey{
		UserId:              1,
		UserRole:            "admin",
		UserChannelGroupIDs: nil,
	}
	candidates := []Candidate{
		{ChannelID: 1, CreatedByUserID: 1, GroupIDs: nil},         // 本人创建 → 可用
		{ChannelID: 2, CreatedByUserID: 9, GroupIDs: nil},         // 他人未分组 → 拒绝
		{ChannelID: 3, CreatedByUserID: 9, GroupIDs: []uint64{7}}, // 他人分组且未加入 → 拒绝
	}
	filtered := filterCandidates(candidates, key, []string{"admin"})
	if len(filtered) != 1 || filtered[0].ChannelID != 1 {
		t.Fatalf("admin should only see own/group channels, got %+v", filtered)
	}
}

func TestRouteCacheCarriesCreator(t *testing.T) {
	if !routeCacheCarriesCreator(nil) {
		t.Fatal("empty cache should be treated as carrying creator")
	}
	if routeCacheCarriesCreator([]Candidate{{ChannelID: 1, CreatedByUserID: 0}}) {
		t.Fatal("legacy cache without creator must force rebuild")
	}
	if !routeCacheCarriesCreator([]Candidate{{ChannelID: 1, CreatedByUserID: 1}}) {
		t.Fatal("cache with creator should be used")
	}
}

func TestCandidatesAllOwnedBy(t *testing.T) {
	if candidatesAllOwnedBy(nil, 1) {
		t.Fatal("empty candidates must not count as all-owned")
	}
	if candidatesAllOwnedBy([]Candidate{{CreatedByUserID: 1}}, 0) {
		t.Fatal("user 0 must not own channels")
	}
	if !candidatesAllOwnedBy([]Candidate{{CreatedByUserID: 1}, {CreatedByUserID: 1}}, 1) {
		t.Fatal("all own candidates should skip balance check")
	}
	if candidatesAllOwnedBy([]Candidate{{CreatedByUserID: 1}, {CreatedByUserID: 2}}, 1) {
		t.Fatal("mixed ownership must still check balance")
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

func TestFilterCandidatesIgnoresConfiguredAdminRoles(t *testing.T) {
	// 管理员角色不再豁免：即使 adminRoles 命中，未创建也未入组仍被过滤。
	key := apikey.AuthKey{UserId: 100, UserRole: "superuser"}
	candidates := []Candidate{{ChannelID: 1, CreatedByUserID: 99, GroupIDs: []uint64{7}}}
	if got := filterCandidates(candidates, key, []string{"superuser"}); len(got) != 0 {
		t.Fatalf("configured admin role must not bypass policy, got %+v", got)
	}
	if got := filterCandidates(candidates, key, []string{"admin"}); len(got) != 0 {
		t.Fatalf("non-admin role should be filtered, got %+v", got)
	}
}
