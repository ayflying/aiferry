package channel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
)

// 渠道级提示缓存处理方式，对应 AdvancedConfig.PromptCacheMode。
const (
	// PromptCacheModeStable 剥离客户端缓存字段，并注入网关生成的稳定缓存键，
	// 让同一用户在同一渠道凭据上的请求共享一个缓存桶，避免跨用户命中彼此的前缀缓存。
	PromptCacheModeStable = "stable"
	// PromptCacheModeOff 只剥离客户端的缓存字段，不下发任何缓存字段。
	// 上游对请求字段做白名单校验（例如返回 UNKNOWN_FIELD）时必须使用。
	PromptCacheModeOff = "off"
	// PromptCacheModePassthrough 完全不干预，缓存字段由客户端自行控制。
	PromptCacheModePassthrough = "passthrough"
)

// promptCacheControlFields 是网关统一接管的缓存控制字段：OpenAI 的
// prompt_cache_key/options/retention 与显式缓存断点。
var promptCacheControlFields = []string{
	"prompt_cache_key", "prompt_cache_options", "prompt_cache_retention", "prompt_cache_breakpoint",
}

// ResolvePromptCacheMode 归并渠道配置里的两个开关。旧的 PassthroughPromptCache
// 优先级更高，保证历史配置语义不变；未声明时按 stable 处理。
func (config AdvancedConfig) ResolvePromptCacheMode() string {
	if config.PassthroughPromptCache {
		return PromptCacheModePassthrough
	}
	switch config.PromptCacheMode {
	case PromptCacheModeOff, PromptCacheModePassthrough:
		return config.PromptCacheMode
	default:
		return PromptCacheModeStable
	}
}

// ApplyPromptCachePolicy 按渠道配置处置请求体中的缓存字段，转发与模型测试共用同一实现，
// 避免「测试通过、正式被上游拒绝」。identity 用于生成稳定缓存键，留空时只剥离不注入。
func ApplyPromptCachePolicy(body []byte, config AdvancedConfig, identity string) ([]byte, error) {
	mode := config.ResolvePromptCacheMode()
	if mode == PromptCacheModePassthrough {
		return body, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, gerror.Wrap(err, "decode prompt cache request")
	}
	removePromptCacheControls(payload)
	if mode == PromptCacheModeStable && identity != "" {
		payload["prompt_cache_key"] = PromptCacheKey(identity)
	}
	result, err := json.Marshal(payload)
	return result, gerror.Wrap(err, "encode prompt cache request")
}

// PromptCacheKey 由调用方给的隔离身份派生稳定缓存键：身份相同则键相同，
// 因此同一用户跨请求命中同一缓存桶，不同用户不会互相污染。
func PromptCacheKey(identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return "aiferry:" + hex.EncodeToString(digest[:16])
}

// removePromptCacheControls 递归删除缓存控制字段：缓存断点会出现在消息内容块里，
// 只处理顶层会漏掉。
func removePromptCacheControls(value any) {
	switch current := value.(type) {
	case map[string]any:
		for _, field := range promptCacheControlFields {
			delete(current, field)
		}
		for _, child := range current {
			removePromptCacheControls(child)
		}
	case []any:
		for _, child := range current {
			removePromptCacheControls(child)
		}
	}
}
