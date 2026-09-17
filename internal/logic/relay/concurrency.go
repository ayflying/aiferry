package relay

import (
	"context"
	"sync"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

// keyConcurrencyWaitWindow 是同一渠道所有密钥的并发额度都占满时，请求最多排队等待的时长。
// 窗口内任意密钥释放额度都会立刻唤醒重试；超过窗口仍无空位则以 429 让客户端稍后重试。
// 该窗口必须明显小于非流式上游超时（默认 600 秒），避免排队时间掩盖上游超时判定。
const keyConcurrencyWaitWindow = 60 * time.Second

// keySlotIdleTTL 与 keySlotSweepInterval 控制长期空闲的「渠道 × 密钥」计数项回收。
const (
	keySlotIdleTTL       = 10 * time.Minute
	keySlotSweepInterval = time.Minute
)

type keySlotKey struct {
	channelID    uint64
	credentialID uint64
}

type keySlotEntry struct {
	active     int
	lastSeenAt time.Time
}

// keySlots 按「渠道 × 密钥」隔离统计在途转发请求数，是渠道并发限制的计数器。
// 额度在进程内计数：网关单容器运行时该值即渠道的真实并发；多副本部署时每个副本
// 各自持有上限（合计为「副本数 × 配置值」），与请求防火墙的并发计数口径一致。
type keySlots struct {
	mu        sync.Mutex
	entries   map[keySlotKey]*keySlotEntry
	released  chan struct{}
	lastSweep time.Time
}

func newKeySlots() *keySlots {
	return &keySlots{entries: make(map[keySlotKey]*keySlotEntry), released: make(chan struct{})}
}

// Notify 返回一个在任意额度释放时被关闭的通道。调用方必须在尝试占用之前取得它：
// 若先尝试占用再取通道，两步之间发生的释放会被漏掉，等待者会白等满整个窗口。
func (l *keySlots) Notify() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.released
}

// TryAcquire 立即占用一个并发额度；该密钥已满额时返回 ok=false，不阻塞调用方。
func (l *keySlots) TryAcquire(channelID, credentialID uint64, limit int) (func(), bool) {
	if limit <= 0 {
		return noopRelease, true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	entry := l.entryFor(keySlotKey{channelID: channelID, credentialID: credentialID}, now)
	if entry.active >= limit {
		return nil, false
	}
	entry.active++
	return func() { l.release(entry) }, true
}

func noopRelease() {}

func (l *keySlots) entryFor(key keySlotKey, now time.Time) *keySlotEntry {
	entry := l.entries[key]
	if entry == nil {
		entry = &keySlotEntry{}
		l.entries[key] = entry
	}
	entry.lastSeenAt = now
	return entry
}

// release 归还额度并唤醒等待者。只让本次占用对应的计数项自减：计数项在 active 归零
// 之前不会被回收，因此该指针不会指向后来新建的同名计数项。
func (l *keySlots) release(entry *keySlotEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry.active > 0 {
		entry.active--
	}
	close(l.released)
	l.released = make(chan struct{})
}

// sweep 回收长期空闲的计数项。active 为 0 表示没有在途请求，删除不会让任何归还动作落空。
func (l *keySlots) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < keySlotSweepInterval {
		return
	}
	l.lastSweep = now
	for key, entry := range l.entries {
		if entry.active == 0 && now.Sub(entry.lastSeenAt) >= keySlotIdleTTL {
			delete(l.entries, key)
		}
	}
}

// credentialPicker 按排除集合挑选一把上游密钥，签名与渠道层的 SelectCredential 一致。
type credentialPicker func(excluded map[uint64]struct{}) (channel.RouteCredential, error)

// acquireKeySlot 为一次上游尝试占用「渠道 × 密钥」的并发额度并返回释放函数。
// 未配置并发限制（limit<=0）时只做挑选，不占用额度，行为与历史完全一致。
func (s *sRelay) acquireKeySlot(ctx context.Context, apiKeyID uint64, candidate Candidate, excluded map[uint64]struct{}) (channel.RouteCredential, func(), error) {
	pick := func(pickExcluded map[uint64]struct{}) (channel.RouteCredential, error) {
		return s.channels.SelectCredential(ctx, apiKeyID, candidate.ChannelID, candidate.ChannelModelID, pickExcluded)
	}
	return acquireKeySlotWith(ctx, s.slots, candidate.ChannelID, candidate.ConcurrencyLimit, keyConcurrencyWaitWindow, excluded, pick)
}

// acquireKeySlotWith 是 acquireKeySlot 的可测核心。额度足够时立即返回；同渠道其它密钥
// 还有空闲额度时改用它（不占用已满密钥的等待窗口）；全部占满后排队等待，任意密钥释放
// 额度即重新挑选；等待超过 waitWindow 返回 ErrChannelConcurrencyExhausted。
func acquireKeySlotWith(ctx context.Context, slots *keySlots, channelID uint64, limit int, waitWindow time.Duration, excluded map[uint64]struct{}, pick credentialPicker) (channel.RouteCredential, func(), error) {
	if slots == nil || limit <= 0 {
		credential, err := pick(excluded)
		return credential, noopRelease, err
	}
	deadline := time.Now().Add(waitWindow)
	saturated := make(map[uint64]struct{})
	for {
		// 每次等待前先取唤醒通道，保证不会漏掉尝试占用期间发生的额度释放。
		notify := slots.Notify()
		for {
			credential, err := pick(mergeExcluded(excluded, saturated))
			if err != nil {
				if len(saturated) == 0 {
					// 渠道确实没有可用密钥（无启用密钥或全部冷却），保持既有错误语义。
					return credential, noopRelease, err
				}
				break
			}
			if _, repeat := saturated[credential.ID]; repeat {
				// 挑选器未遵守排除集合时立即转等待，避免在此处打转。
				break
			}
			if release, ok := slots.TryAcquire(channelID, credential.ID, limit); ok {
				return credential, release, nil
			}
			saturated[credential.ID] = struct{}{}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return channel.RouteCredential{}, noopRelease, keySlotExhaustedError(channelID)
		}
		timer := time.NewTimer(remaining)
		select {
		case <-notify:
			timer.Stop()
			// 有密钥腾出额度，清空占满集合后重新挑选。
			clear(saturated)
		case <-ctx.Done():
			timer.Stop()
			return channel.RouteCredential{}, noopRelease, ctx.Err()
		case <-timer.C:
			return channel.RouteCredential{}, noopRelease, keySlotExhaustedError(channelID)
		}
	}
}

// mergeExcluded 把已满额的密钥并入排除集合。extra 为空时原样返回 base，以保留调用方
// 传入 nil（仅走绑定优先、不写回绑定）与传入空 map（重新绑定）之间的既有行为差异。
func mergeExcluded(base, extra map[uint64]struct{}) map[uint64]struct{} {
	if len(extra) == 0 {
		return base
	}
	merged := make(map[uint64]struct{}, len(base)+len(extra))
	for id := range base {
		merged[id] = struct{}{}
	}
	for id := range extra {
		merged[id] = struct{}{}
	}
	return merged
}

func keySlotExhaustedError(channelID uint64) error {
	return gerror.Wrapf(ErrChannelConcurrencyExhausted, "channel #%d key concurrency exhausted", channelID)
}
