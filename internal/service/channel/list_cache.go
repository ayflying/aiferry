package channel

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yunloli/aiferry/internal/consts"
)

const (
	channelListCacheKey = consts.ChannelListCacheKey
	channelListCacheTTL = 24 * time.Hour
)

func (s *Service) readListCache(ctx context.Context) ([]View, bool) {
	if s.app == nil || s.app.Redis == nil {
		return nil, false
	}
	encoded, err := s.app.Redis.Get(ctx, channelListCacheKey).Bytes()
	if err != nil {
		return nil, false
	}
	views, ok := decodeListCache(encoded)
	if !ok {
		_ = s.app.Redis.Del(ctx, channelListCacheKey).Err()
	}
	return views, ok
}

func (s *Service) writeListCache(ctx context.Context, views []View) {
	if s.app == nil || s.app.Redis == nil {
		return
	}
	encoded, err := json.Marshal(views)
	if err == nil {
		_ = s.app.Redis.Set(ctx, channelListCacheKey, encoded, channelListCacheTTL).Err()
	}
}

func (s *Service) InvalidateListCache(ctx context.Context) {
	if s.app != nil && s.app.Redis != nil {
		_ = s.app.Redis.Del(ctx, channelListCacheKey).Err()
	}
}

func decodeListCache(encoded []byte) ([]View, bool) {
	views := make([]View, 0)
	if err := json.Unmarshal(encoded, &views); err != nil {
		return nil, false
	}
	return views, true
}
