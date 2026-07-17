package usage

import (
	"strings"

	"github.com/tidwall/gjson"
)

func ParseJSONUsage(body []byte) TokenUsage {
	input := optionalUint(body, "usage.input_tokens", "usage.prompt_tokens", "response.usage.input_tokens")
	cached := optionalUint(body, "usage.input_tokens_details.cached_tokens", "usage.prompt_tokens_details.cached_tokens", "response.usage.input_tokens_details.cached_tokens")
	cacheWrite := optionalUint(body, "usage.cache_creation_input_tokens", "usage.cache_creation_tokens", "usage.input_tokens_details.cache_creation_tokens", "usage.prompt_tokens_details.cache_creation_tokens")
	imageInput := optionalUint(body, "usage.image_tokens", "usage.input_tokens_details.image_tokens", "usage.prompt_tokens_details.image_tokens")
	audioInput := optionalUint(body, "usage.audio_tokens", "usage.input_tokens_details.audio_tokens", "usage.prompt_tokens_details.audio_tokens")
	output := optionalUint(body, "usage.output_tokens", "usage.completion_tokens", "response.usage.output_tokens")
	audioOutput := optionalUint(body, "usage.output_audio_tokens", "usage.output_tokens_details.audio_tokens", "usage.completion_tokens_details.audio_tokens")
	total := optionalUint(body, "usage.total_tokens", "response.usage.total_tokens")
	if total == nil && input != nil && output != nil {
		value := *input + *output
		total = &value
	}
	return TokenUsage{Input: input, CachedInput: cached, CacheWrite: cacheWrite, ImageInput: imageInput, AudioInput: audioInput, Output: output, AudioOutput: audioOutput, Total: total}
}

func ParseSSEUsage(line []byte, target *TokenUsage) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if payload == "" || payload == "[DONE]" || !gjson.Valid(payload) {
		return
	}
	parsed := ParseJSONUsage([]byte(payload))
	if hasTokenUsage(parsed) {
		*target = parsed
	}
}

func optionalUint(body []byte, paths ...string) *uint64 {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Exists() && value.Type == gjson.Number {
			number := value.Uint()
			return &number
		}
	}
	return nil
}

func hasTokenUsage(tokens TokenUsage) bool {
	return tokens.Input != nil || tokens.CachedInput != nil || tokens.CacheWrite != nil || tokens.ImageInput != nil || tokens.AudioInput != nil || tokens.Output != nil || tokens.AudioOutput != nil
}
