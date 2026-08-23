package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channel"
)

// applyPromptCachePolicy makes cache ownership explicit per channel.
// When passthrough is enabled, callers fully control all prompt_cache fields.
// Otherwise AiFerry removes caller-provided cache controls and sets one stable
// cache key for the user, public model, selected channel, and selected credential.
func applyPromptCachePolicy(body []byte, candidate Candidate, userID uint64, config channel.AdvancedConfig) ([]byte, error) {
	if config.PassthroughPromptCache {
		return body, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, gerror.Wrap(err, "decode prompt cache request")
	}
	removePromptCacheControls(payload)
	payload["prompt_cache_key"] = stablePromptCacheKey(userID, candidate)
	result, err := json.Marshal(payload)
	return result, gerror.Wrap(err, "encode prompt cache request")
}

func stablePromptCacheKey(userID uint64, candidate Candidate) string {
	identity := fmt.Sprintf("v1|u:%d|m:%s|c:%d|k:%d", userID, candidate.PublicName, candidate.ChannelID, candidate.ChannelCredentialID)
	digest := sha256.Sum256([]byte(identity))
	return "aiferry:" + hex.EncodeToString(digest[:16])
}

func removePromptCacheControls(value any) {
	switch current := value.(type) {
	case map[string]any:
		delete(current, "prompt_cache_key")
		delete(current, "prompt_cache_options")
		delete(current, "prompt_cache_retention")
		delete(current, "prompt_cache_breakpoint")
		for _, child := range current {
			removePromptCacheControls(child)
		}
	case []any:
		for _, child := range current {
			removePromptCacheControls(child)
		}
	}
}
