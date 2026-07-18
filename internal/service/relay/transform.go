package relay

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/service/channel"
)

func prepareRequestBody(endpoint string, originalBody []byte, upstreamModel string, config channel.AdvancedConfig) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(originalBody, &payload); err != nil {
		return nil, gerror.Wrap(err, "decode request body")
	}
	if !config.PassthroughRequestBody {
		payload = supportedRequestFields(endpoint, payload, config)
	}
	applyRestrictedFields(payload, config)
	applyDefaultSystemPrompt(endpoint, payload, config)
	payload["model"] = upstreamModel
	if endpoint == "/chat/completions" && boolValue(payload["stream"]) {
		options, _ := payload["stream_options"].(map[string]any)
		if options == nil {
			options = make(map[string]any)
			payload["stream_options"] = options
		}
		options["include_usage"] = true
	}
	result, err := json.Marshal(payload)
	return result, gerror.Wrap(err, "encode upstream request body")
}

func supportedRequestFields(endpoint string, payload map[string]any, config channel.AdvancedConfig) map[string]any {
	fields := map[string]struct{}{
		"model": {}, "stream": {}, "user": {},
	}
	switch endpoint {
	case "/chat/completions":
		for _, field := range []string{"messages", "audio", "frequency_penalty", "logit_bias", "logprobs", "max_completion_tokens", "max_tokens", "modalities", "n", "parallel_tool_calls", "prediction", "presence_penalty", "reasoning_effort", "response_format", "seed", "stop", "stream_options", "temperature", "tool_choice", "tools", "top_logprobs", "top_p", "web_search_options"} {
			fields[field] = struct{}{}
		}
	case "/responses":
		for _, field := range []string{"input", "instructions", "background", "max_output_tokens", "max_tool_calls", "metadata", "parallel_tool_calls", "previous_response_id", "prompt", "reasoning", "temperature", "text", "tool_choice", "tools", "top_logprobs", "top_p", "truncation"} {
			fields[field] = struct{}{}
		}
	case "/embeddings":
		for _, field := range []string{"input", "dimensions", "encoding_format"} {
			fields[field] = struct{}{}
		}
	}
	result := make(map[string]any, len(fields))
	for field := range fields {
		if value, ok := payload[field]; ok {
			result[field] = value
		}
	}
	for field, enabled := range map[string]bool{
		"service_tier":      config.AllowServiceTier,
		"store":             !config.BlockStore,
		"safety_identifier": config.AllowSafetyIdentifier,
		"include":           config.AllowInclude,
		"inference_geo":     config.AllowInferenceGeo,
	} {
		if value, ok := payload[field]; enabled && ok {
			result[field] = value
		}
	}
	return result
}

func applyRestrictedFields(payload map[string]any, config channel.AdvancedConfig) {
	if !config.AllowServiceTier {
		delete(payload, "service_tier")
	}
	if config.BlockStore {
		delete(payload, "store")
	}
	if !config.AllowSafetyIdentifier {
		delete(payload, "safety_identifier")
	}
	if !config.AllowInclude {
		delete(payload, "include")
	}
	if !config.AllowInferenceGeo {
		delete(payload, "inference_geo")
	}
}

func applyDefaultSystemPrompt(endpoint string, payload map[string]any, config channel.AdvancedConfig) {
	if config.SystemPrompt == "" {
		return
	}
	switch endpoint {
	case "/chat/completions":
		applyChatSystemPrompt(payload, config)
	case "/responses":
		applyResponseInstructions(payload, config)
	}
}

func applyChatSystemPrompt(payload map[string]any, config channel.AdvancedConfig) {
	messages, ok := payload["messages"].([]any)
	if !ok {
		return
	}
	for _, value := range messages {
		message, ok := value.(map[string]any)
		if !ok || (message["role"] != "system" && message["role"] != "developer") {
			continue
		}
		content, ok := message["content"].(string)
		if !ok || !config.AppendSystemPrompt {
			return
		}
		message["content"] = joinSystemPrompt(config.SystemPrompt, content)
		return
	}
	payload["messages"] = append([]any{map[string]any{"role": "system", "content": config.SystemPrompt}}, messages...)
}

func applyResponseInstructions(payload map[string]any, config channel.AdvancedConfig) {
	instructions, _ := payload["instructions"].(string)
	if instructions == "" {
		payload["instructions"] = config.SystemPrompt
		return
	}
	if config.AppendSystemPrompt {
		payload["instructions"] = joinSystemPrompt(config.SystemPrompt, instructions)
	}
}

func joinSystemPrompt(channelPrompt, userPrompt string) string {
	return channelPrompt + "\n\n" + userPrompt
}

func normalizeResponseBody(endpoint string, body []byte, upstreamModel string, config channel.AdvancedConfig) []byte {
	if !config.ForceOpenAIFormat && !config.ReasoningToContent {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if config.ForceOpenAIFormat {
		applyOpenAIResponseMetadata(endpoint, payload, upstreamModel)
	}
	if config.ReasoningToContent {
		moveReasoningToContent(payload)
	}
	result, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return result
}

func normalizeSSELine(endpoint string, line []byte, upstreamModel string, config channel.AdvancedConfig) []byte {
	if !config.ForceOpenAIFormat && !config.ReasoningToContent {
		return line
	}
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return line
	}
	payload := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if payload == "" || payload == "[DONE]" {
		return line
	}
	normalized := normalizeResponseBody(endpoint, []byte(payload), upstreamModel, config)
	if string(normalized) == payload {
		return line
	}
	return []byte("data: " + string(normalized) + "\n")
}

func applyOpenAIResponseMetadata(endpoint string, payload map[string]any, upstreamModel string) {
	if _, ok := payload["model"]; !ok && upstreamModel != "" {
		payload["model"] = upstreamModel
	}
	if _, ok := payload["created"]; !ok {
		payload["created"] = time.Now().Unix()
	}
	if _, ok := payload["object"]; ok {
		return
	}
	switch endpoint {
	case "/chat/completions":
		payload["object"] = "chat.completion"
	case "/responses":
		payload["object"] = "response"
	case "/embeddings":
		payload["object"] = "list"
	}
}

func moveReasoningToContent(payload map[string]any) {
	choices, ok := payload["choices"].([]any)
	if !ok {
		return
	}
	for _, value := range choices {
		choice, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"message", "delta"} {
			container, ok := choice[key].(map[string]any)
			if !ok {
				continue
			}
			reasoning, ok := container["reasoning_content"].(string)
			if !ok || reasoning == "" {
				continue
			}
			content, exists := container["content"]
			if exists {
				var text bool
				content, text = content.(string)
				if !text {
					continue
				}
			}
			text, _ := content.(string)
			container["content"] = "<think>" + reasoning + "</think>" + text
			delete(container, "reasoning_content")
		}
	}
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}
