package channelgroup

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	"github.com/gogf/gf/v2/database/gdb"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/dao"
)

// This test talks to a configured database only when explicitly requested.
// It creates a disabled, temporary group and removes it during cleanup.
func TestChannelGroupMembershipRoundTripIntegration(t *testing.T) {
	if os.Getenv("AIFERRY_INTEGRATION_TEST") != "1" {
		t.Skip("set AIFERRY_INTEGRATION_TEST=1 to run against a configured database")
	}

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load integration configuration: %v", err)
	}
	if err = gdb.AddConfigNode("default", gdb.ConfigNode{Type: "mysql", Link: cfg.GoFrameMySQLLink()}); err != nil {
		t.Fatalf("configure integration database: %v", err)
	}

	type integrationChannel struct {
		ID uint64 `orm:"id"`
	}
	channels := make([]integrationChannel, 0, 2)
	if err = dao.Channels.Ctx(ctx).
		Fields(dao.Channels.Columns().Id).
		OrderAsc(dao.Channels.Columns().Id).
		Limit(2).
		Scan(&channels); err != nil {
		t.Fatalf("load channels for integration test: %v", err)
	}
	if len(channels) < 2 || channels[0].ID == 0 || channels[1].ID == 0 {
		t.Fatal("integration test requires at least two channels")
	}
	channelIDs := []uint64{channels[0].ID, channels[1].ID}

	service := New()
	input := adminapi.ChannelGroupInput{
		Name:        "integration channel group",
		Code:        fmt.Sprintf("integration-%d", time.Now().UnixNano()),
		Description: "temporary channel group integration test",
		Status:      0,
		ChannelIDs:  []uint64{channelIDs[0]},
	}
	groupID, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("create temporary channel group: %v", err)
	}
	t.Cleanup(func() {
		if cleanupErr := service.Delete(ctx, groupID); cleanupErr != nil {
			t.Errorf("delete temporary channel group %d: %v", groupID, cleanupErr)
		}
	})

	assertGroupChannels(t, service, ctx, groupID, []uint64{channelIDs[0]})
	input.ChannelIDs = []uint64{channelIDs[1]}
	if err = service.Update(ctx, groupID, input); err != nil {
		t.Fatalf("update temporary channel group: %v", err)
	}
	assertGroupChannels(t, service, ctx, groupID, []uint64{channelIDs[1]})
	groupIDs, err := service.ChannelIDs(ctx, channelIDs[1])
	if err != nil {
		t.Fatalf("list channel memberships after update: %v", err)
	}
	if !containsID(groupIDs, groupID) {
		t.Fatalf("channel %d group IDs = %v, want group %d", channelIDs[1], groupIDs, groupID)
	}
}

func assertGroupChannels(t *testing.T, service *sChannelGroup, ctx context.Context, groupID uint64, expected []uint64) {
	t.Helper()
	groups, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list channel groups: %v", err)
	}
	for _, group := range groups {
		if group.Id != groupID {
			continue
		}
		if sameIDs(group.ChannelIDs, expected) {
			return
		}
		t.Fatalf("group %d channel IDs = %v, want %v", groupID, group.ChannelIDs, expected)
	}
	t.Fatalf("temporary channel group %d was not returned by list", groupID)
}

func sameIDs(left, right []uint64) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]uint64(nil), left...)
	right = append([]uint64(nil), right...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsID(values []uint64, wanted uint64) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
