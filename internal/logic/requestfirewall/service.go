package requestfirewall

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/system"
)

const (
	settingsRefreshInterval = 30 * time.Second
	entryIdleTTL            = 10 * time.Minute
	cleanupInterval         = time.Minute
)

type RequestInput struct {
	ClientIP string
	APIKeyID uint64
}

type Rejection struct {
	Message    string
	RetryAfter int
}

type limiterEntry struct {
	active     int
	tokens     float64
	updatedAt  time.Time
	lastSeenAt time.Time
}

type sRequestFirewall struct {
	settings *system.Service

	mu          sync.Mutex
	config      adminapi.RequestFirewallSettingsInput
	nextRefresh time.Time
	lastCleanup time.Time
	active      int
	ips         map[string]*limiterEntry
	apiKeys     map[uint64]*limiterEntry
}

func New(settings *system.Service) *sRequestFirewall {
	return &sRequestFirewall{
		settings: settings,
		config:   system.DefaultRequestFirewallSettings(),
		ips:      make(map[string]*limiterEntry),
		apiKeys:  make(map[uint64]*limiterEntry),
	}
}

func (s *sRequestFirewall) SetSettings(settings adminapi.RequestFirewallSettingsInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = settings
	s.nextRefresh = time.Now().Add(settingsRefreshInterval)
}

func (s *sRequestFirewall) Acquire(ctx context.Context, input RequestInput) (func(), *Rejection) {
	s.refresh(ctx)
	return s.acquireAt(input, time.Now())
}

// AcquireClient admits a request before API key authentication. This makes the
// global and per-IP limits effective for malformed and invalid-key requests.
func (s *sRequestFirewall) AcquireClient(ctx context.Context, input RequestInput) (func(), *Rejection) {
	s.refresh(ctx)
	return s.acquireClientAt(input.ClientIP, time.Now())
}

// AcquireAPIKey applies the API-key-specific limits after authentication.
// The caller must already hold a client admission while this release function
// remains active.
func (s *sRequestFirewall) AcquireAPIKey(input RequestInput) (func(), *Rejection) {
	return s.acquireAPIKeyAt(input.APIKeyID, time.Now())
}

func (s *sRequestFirewall) refresh(ctx context.Context) {
	if s.settings == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	if now.Before(s.nextRefresh) {
		s.mu.Unlock()
		return
	}
	s.nextRefresh = now.Add(settingsRefreshInterval)
	s.mu.Unlock()

	settings, err := s.settings.GetRequestFirewallSettings(ctx)
	if err == nil {
		s.SetSettings(settings)
	}
}

func (s *sRequestFirewall) acquireAt(input RequestInput, now time.Time) (func(), *Rejection) {
	clientRelease, rejection := s.acquireClientAt(input.ClientIP, now)
	if rejection != nil {
		return nil, rejection
	}
	apiKeyRelease, rejection := s.acquireAPIKeyAt(input.APIKeyID, now)
	if rejection != nil {
		clientRelease()
		return nil, rejection
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			apiKeyRelease()
			clientRelease()
		})
	}, nil
}

func (s *sRequestFirewall) acquireClientAt(clientIP string, now time.Time) (func(), *Rejection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled {
		return func() {}, nil
	}
	s.cleanup(now)
	if s.active >= s.config.MaxConcurrentRequests {
		return nil, rejected("全局并发请求已达到安全限制", 1)
	}

	ip := normalizeClientIP(clientIP)
	ipEntry := s.entryForIP(ip, now)
	if ipEntry.active >= s.config.MaxConcurrentRequestsPerIP {
		return nil, rejected("当前 IP 并发请求已达到安全限制", 1)
	}
	refill(ipEntry, now, s.config.RequestsPerMinutePerIP)
	if ipEntry.tokens < 1 {
		return nil, rejected("当前 IP 请求频率已达到安全限制", retryAfter(ipEntry, s.config.RequestsPerMinutePerIP))
	}
	ipEntry.tokens--
	ipEntry.active++
	s.active++

	var once sync.Once
	return func() {
		once.Do(func() { s.releaseClient(ip) })
	}, nil
}

func (s *sRequestFirewall) acquireAPIKeyAt(apiKeyID uint64, now time.Time) (func(), *Rejection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled {
		return func() {}, nil
	}
	s.cleanup(now)
	keyEntry := s.entryForAPIKey(apiKeyID, now)
	if keyEntry.active >= s.config.MaxConcurrentRequestsPerKey {
		return nil, rejected("当前密钥并发请求已达到安全限制", 1)
	}
	refill(keyEntry, now, s.config.RequestsPerMinutePerAPIKey)
	if keyEntry.tokens < 1 {
		return nil, rejected("当前密钥请求频率已达到安全限制", retryAfter(keyEntry, s.config.RequestsPerMinutePerAPIKey))
	}
	keyEntry.tokens--
	keyEntry.active++

	var once sync.Once
	return func() {
		once.Do(func() { s.releaseAPIKey(apiKeyID) })
	}, nil
}

func (s *sRequestFirewall) releaseClient(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.ips[ip]; entry != nil && entry.active > 0 {
		entry.active--
	}
	if s.active > 0 {
		s.active--
	}
}

func (s *sRequestFirewall) releaseAPIKey(apiKeyID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.apiKeys[apiKeyID]; entry != nil && entry.active > 0 {
		entry.active--
	}
}

func normalizeClientIP(ip string) string {
	if ip = strings.TrimSpace(ip); ip == "" {
		return "unknown"
	}
	return ip
}

func (s *sRequestFirewall) entryForIP(ip string, now time.Time) *limiterEntry {
	entry := s.ips[ip]
	if entry == nil {
		entry = &limiterEntry{}
		s.ips[ip] = entry
	}
	entry.lastSeenAt = now
	return entry
}

func (s *sRequestFirewall) entryForAPIKey(apiKeyID uint64, now time.Time) *limiterEntry {
	entry := s.apiKeys[apiKeyID]
	if entry == nil {
		entry = &limiterEntry{}
		s.apiKeys[apiKeyID] = entry
	}
	entry.lastSeenAt = now
	return entry
}

func (s *sRequestFirewall) cleanup(now time.Time) {
	if now.Sub(s.lastCleanup) < cleanupInterval {
		return
	}
	s.lastCleanup = now
	for ip, entry := range s.ips {
		if entry.active == 0 && now.Sub(entry.lastSeenAt) >= entryIdleTTL {
			delete(s.ips, ip)
		}
	}
	for apiKeyID, entry := range s.apiKeys {
		if entry.active == 0 && now.Sub(entry.lastSeenAt) >= entryIdleTTL {
			delete(s.apiKeys, apiKeyID)
		}
	}
}

func refill(entry *limiterEntry, now time.Time, perMinute int) {
	capacity := float64(perMinute)
	if entry.updatedAt.IsZero() {
		entry.tokens = capacity
		entry.updatedAt = now
		return
	}
	elapsed := now.Sub(entry.updatedAt).Seconds()
	entry.tokens = math.Min(capacity, entry.tokens+elapsed*capacity/60)
	entry.updatedAt = now
}

func retryAfter(entry *limiterEntry, perMinute int) int {
	seconds := (1 - entry.tokens) * 60 / float64(perMinute)
	return max(1, int(math.Ceil(seconds)))
}

func rejected(message string, retryAfterSeconds int) *Rejection {
	return &Rejection{Message: message, RetryAfter: max(1, retryAfterSeconds)}
}
