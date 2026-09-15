package relay

import (
	"fmt"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

// applyPromptCachePolicy 让缓存归属在渠道层显式化：默认按用户、公开模型、渠道与
// 凭据生成稳定缓存键；渠道声明 off 时只剥离、不下发任何缓存字段；声明 passthrough
// 时不干预。策略实现放在渠道层，与模型测试共用同一份，避免「测试通过、正式被拒」。
func applyPromptCachePolicy(body []byte, candidate Candidate, userID uint64, config channel.AdvancedConfig) ([]byte, error) {
	return channel.ApplyPromptCachePolicy(body, config, promptCacheIdentity(userID, candidate))
}

// promptCacheIdentity 是稳定缓存键的隔离身份：四项全部相同才共享同一个缓存桶，
// 因此同一用户的连续请求命中同一缓存，不同用户之间不会互相污染前缀缓存。
func promptCacheIdentity(userID uint64, candidate Candidate) string {
	return fmt.Sprintf("v1|u:%d|m:%s|c:%d|k:%d", userID, candidate.PublicName, candidate.ChannelID, candidate.ChannelCredentialID)
}
