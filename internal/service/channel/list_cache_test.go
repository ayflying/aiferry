package channel

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestDecodeListCachePreservesViews(t *testing.T) {
	used := 1.25
	want := []View{{
		Id:                7,
		Name:              "primary",
		EnabledModelCount: 3,
		CredentialCount:   2,
		CostSummaries:     []CostSummary{{Currency: "USD", UsedAmount: &used}},
		GroupIDs:          []uint64{4, 9},
	}}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decodeListCache(encoded)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected cached views: got %#v, want %#v", got, want)
	}
}

func TestDecodeListCacheRejectsInvalidJSON(t *testing.T) {
	if _, ok := decodeListCache([]byte("not-json")); ok {
		t.Fatal("invalid cache content must be rejected")
	}
}

func TestChannelListCacheTTL(t *testing.T) {
	if channelListCacheTTL != 24*time.Hour {
		t.Fatalf("unexpected cache TTL: %s", channelListCacheTTL)
	}
}
