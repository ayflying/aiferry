package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

func credentialOf(id uint64) channel.RouteCredential {
	return channel.RouteCredential{ID: id, APIKeyCipher: "cipher"}
}

// 同一渠道的多把密钥额度互相隔离：limit=2 时两把密钥各能占 2 个额度，合计 4。
func TestKeySlotsIsolatePerCredential(t *testing.T) {
	slots := newKeySlots()
	releases := make([]func(), 0, 4)
	for _, credentialID := range []uint64{11, 11, 22, 22} {
		release, ok := slots.TryAcquire(7, credentialID, 2)
		if !ok {
			t.Fatalf("credential %d should have free capacity", credentialID)
		}
		releases = append(releases, release)
	}
	if _, ok := slots.TryAcquire(7, 11, 2); ok {
		t.Fatal("credential 11 exceeded its own limit")
	}
	if _, ok := slots.TryAcquire(7, 22, 2); ok {
		t.Fatal("credential 22 exceeded its own limit")
	}
	for _, release := range releases {
		release()
	}
	release, ok := slots.TryAcquire(7, 11, 2)
	if !ok {
		t.Fatal("capacity was not restored after releasing every slot")
	}
	release()
}

// 不同渠道的同名密钥编号互不影响。
func TestKeySlotsIsolatePerChannel(t *testing.T) {
	slots := newKeySlots()
	releaseA, ok := slots.TryAcquire(1, 5, 1)
	if !ok {
		t.Fatal("channel 1 credential 5 should be admitted")
	}
	defer releaseA()
	if _, ok = slots.TryAcquire(2, 5, 1); !ok {
		t.Fatal("channel 2 credential 5 must not share channel 1 capacity")
	}
}

func TestKeySlotsTreatsZeroLimitAsUnlimited(t *testing.T) {
	slots := newKeySlots()
	for index := 0; index < 100; index++ {
		if _, ok := slots.TryAcquire(3, 9, 0); !ok {
			t.Fatal("limit 0 must not reject any request")
		}
	}
}

// 调低限制后不得超额放行：已在途的请求继续持有额度，直到计数降到新限制以下。
func TestKeySlotsLoweredLimitDoesNotOverAdmit(t *testing.T) {
	slots := newKeySlots()
	releases := make([]func(), 0, 3)
	for index := 0; index < 3; index++ {
		release, ok := slots.TryAcquire(4, 8, 3)
		if !ok {
			t.Fatalf("acquire %d should be admitted", index)
		}
		releases = append(releases, release)
	}
	if _, ok := slots.TryAcquire(4, 8, 1); ok {
		t.Fatal("lowered limit must not admit while in-flight requests exceed it")
	}
	for _, release := range releases {
		release()
	}
	release, ok := slots.TryAcquire(4, 8, 1)
	if !ok {
		t.Fatal("capacity should be available once in-flight requests drain")
	}
	release()
}

// 调高限制后立即生效。
func TestKeySlotsRaisedLimitApplies(t *testing.T) {
	slots := newKeySlots()
	release, ok := slots.TryAcquire(6, 12, 1)
	if !ok {
		t.Fatal("first slot should be admitted")
	}
	defer release()
	if _, ok = slots.TryAcquire(6, 12, 1); ok {
		t.Fatal("limit 1 should be exhausted")
	}
	second, ok := slots.TryAcquire(6, 12, 2)
	if !ok {
		t.Fatal("raised limit should admit another request")
	}
	second()
}

// 释放会唤醒等待者；长期空闲的计数项被回收，在途计数项不会被回收。
func TestKeySlotsSweepKeepsActiveEntries(t *testing.T) {
	slots := newKeySlots()
	staleRelease, ok := slots.TryAcquire(8, 1, 4)
	if !ok {
		t.Fatal("active entry should be admitted")
	}
	staleRelease()
	if _, ok = slots.TryAcquire(8, 2, 4); !ok {
		t.Fatal("second entry should be admitted")
	}
	slots.mu.Lock()
	for key := range slots.entries {
		slots.entries[key].lastSeenAt = time.Now().Add(-2 * keySlotIdleTTL)
	}
	slots.lastSweep = time.Time{}
	slots.mu.Unlock()
	// 触发一次清扫：idle 且无在途的条目被删除，持有额度的条目保留。
	release, ok := slots.TryAcquire(8, 3, 4)
	if !ok {
		t.Fatal("third entry should be admitted")
	}
	release()
	slots.mu.Lock()
	defer slots.mu.Unlock()
	if _, exists := slots.entries[keySlotKey{channelID: 8, credentialID: 1}]; exists {
		t.Fatal("idle entry should be swept")
	}
	if _, exists := slots.entries[keySlotKey{channelID: 8, credentialID: 2}]; !exists {
		t.Fatal("entry holding a slot must not be swept")
	}
}

func TestAcquireKeySlotSkipsUnlimitedChannel(t *testing.T) {
	pickCalls := 0
	pick := func(excluded map[uint64]struct{}) (channel.RouteCredential, error) {
		pickCalls++
		if len(excluded) != 0 {
			t.Fatalf("unlimited channel must not exclude credentials: %v", excluded)
		}
		return credentialOf(1), nil
	}
	credential, release, err := acquireKeySlotWith(context.Background(), newKeySlots(), 1, 0, time.Second, nil, pick)
	if err != nil || credential.ID != 1 {
		t.Fatalf("acquire = (%v, %v), want credential 1 without error", credential, err)
	}
	release()
	if pickCalls != 1 {
		t.Fatalf("picker calls = %d, want 1", pickCalls)
	}
}

// 首选密钥已满时改用同渠道另一把有空闲额度的密钥，而不是排队等待。
func TestAcquireKeySlotSpreadsToIdleCredential(t *testing.T) {
	slots := newKeySlots()
	busyRelease, ok := slots.TryAcquire(1, 1, 1)
	if !ok {
		t.Fatal("credential 1 should be admitted first")
	}
	defer busyRelease()
	pick := func(excluded map[uint64]struct{}) (channel.RouteCredential, error) {
		if _, skip := excluded[1]; skip {
			return credentialOf(2), nil
		}
		return credentialOf(1), nil
	}
	credential, release, err := acquireKeySlotWith(context.Background(), slots, 1, 1, time.Second, nil, pick)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	defer release()
	if credential.ID != 2 {
		t.Fatalf("credential = %d, want the idle credential 2", credential.ID)
	}
}

// 全部密钥满额时排队等待，任意密钥释放额度即被唤醒并继续。
func TestAcquireKeySlotWaitsForReleasedSlot(t *testing.T) {
	slots := newKeySlots()
	busyRelease, ok := slots.TryAcquire(1, 1, 1)
	if !ok {
		t.Fatal("credential 1 should be admitted first")
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		busyRelease()
	}()
	pick := func(map[uint64]struct{}) (channel.RouteCredential, error) { return credentialOf(1), nil }
	credential, release, err := acquireKeySlotWith(context.Background(), slots, 1, 1, 2*time.Second, nil, pick)
	if err != nil {
		t.Fatalf("acquire() error = %v, want success after the slot is released", err)
	}
	release()
	if credential.ID != 1 {
		t.Fatalf("credential = %d, want 1", credential.ID)
	}
}

// 等待窗口内没有空位时返回并发额度耗尽错误，而不是上游错误。
func TestAcquireKeySlotTimesOutWhenFullySaturated(t *testing.T) {
	slots := newKeySlots()
	busyRelease, ok := slots.TryAcquire(1, 1, 1)
	if !ok {
		t.Fatal("credential 1 should be admitted first")
	}
	defer busyRelease()
	pick := func(map[uint64]struct{}) (channel.RouteCredential, error) { return credentialOf(1), nil }
	_, _, err := acquireKeySlotWith(context.Background(), slots, 1, 1, 40*time.Millisecond, nil, pick)
	if !errors.Is(err, ErrChannelConcurrencyExhausted) {
		t.Fatalf("acquire() error = %v, want ErrChannelConcurrencyExhausted", err)
	}
}

// 请求上下文取消时立即返回，不白等整个等待窗口。
func TestAcquireKeySlotHonoursContextCancellation(t *testing.T) {
	slots := newKeySlots()
	busyRelease, ok := slots.TryAcquire(1, 1, 1)
	if !ok {
		t.Fatal("credential 1 should be admitted first")
	}
	defer busyRelease()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	pick := func(map[uint64]struct{}) (channel.RouteCredential, error) { return credentialOf(1), nil }
	startedAt := time.Now()
	_, _, err := acquireKeySlotWith(ctx, slots, 1, 1, 5*time.Second, nil, pick)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("acquire() blocked for %s after cancellation", elapsed)
	}
}

// 渠道确实没有可用密钥（无启用密钥或全部冷却）时保持既有错误语义，不转成限流错误。
func TestAcquireKeySlotKeepsPickerErrorWhenNoCredential(t *testing.T) {
	noCredential := errors.New("channel has no available upstream credential")
	pick := func(map[uint64]struct{}) (channel.RouteCredential, error) {
		return channel.RouteCredential{}, noCredential
	}
	_, _, err := acquireKeySlotWith(context.Background(), newKeySlots(), 1, 4, 40*time.Millisecond, map[uint64]struct{}{}, pick)
	if !errors.Is(err, noCredential) {
		t.Fatalf("acquire() error = %v, want the picker error", err)
	}
}

// 并发压测：limit=3 时始终只有 3 个请求同时持有额度，且密钥之间互不占用。
func TestAcquireKeySlotEnforcesLimitUnderConcurrency(t *testing.T) {
	slots := newKeySlots()
	var (
		mu      sync.Mutex
		current = map[uint64]int{}
		peak    = map[uint64]int{}
		wait    sync.WaitGroup
	)
	pick := func(excluded map[uint64]struct{}) (channel.RouteCredential, error) {
		for _, credentialID := range []uint64{1, 2} {
			if _, skip := excluded[credentialID]; !skip {
				return credentialOf(credentialID), nil
			}
		}
		return channel.RouteCredential{}, errors.New("no credential")
	}
	for index := 0; index < 24; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			credential, release, err := acquireKeySlotWith(context.Background(), slots, 1, 3, 3*time.Second, nil, pick)
			if err != nil {
				t.Errorf("acquire() error = %v", err)
				return
			}
			mu.Lock()
			current[credential.ID]++
			if current[credential.ID] > peak[credential.ID] {
				peak[credential.ID] = current[credential.ID]
			}
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			current[credential.ID]--
			mu.Unlock()
			release()
		}()
	}
	wait.Wait()
	for credentialID, value := range peak {
		if value > 3 {
			t.Fatalf("credential %d reached %d concurrent requests, limit is 3", credentialID, value)
		}
	}
}

func TestMergeExcludedPreservesCallerSemantics(t *testing.T) {
	if merged := mergeExcluded(nil, nil); merged != nil {
		t.Fatalf("mergeExcluded(nil, nil) = %#v, want nil", merged)
	}
	base := map[uint64]struct{}{1: {}}
	empty := map[uint64]struct{}{}
	if merged := mergeExcluded(empty, nil); merged == nil || len(merged) != 0 {
		t.Fatalf("mergeExcluded(empty, nil) = %#v, want an empty non-nil map", merged)
	}
	merged := mergeExcluded(base, map[uint64]struct{}{2: {}})
	if len(merged) != 2 {
		t.Fatalf("merged = %#v, want both exclusions", merged)
	}
	if len(base) != 1 {
		t.Fatalf("mergeExcluded must not mutate the caller map: %#v", base)
	}
}
