package relay

import (
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
)

const (
	chatCompletionsEndpoint = "/chat/completions"
	responsesEndpoint       = "/responses"

	chatToResponsesConversion = "chat_to_responses"
	responsesToChatConversion = "responses_to_chat"
)

type protocolPlan struct {
	clientEndpoint   string
	upstreamEndpoint string
	conversion       string
}

func directProtocolPlan(endpoint string) protocolPlan {
	return protocolPlan{clientEndpoint: endpoint, upstreamEndpoint: endpoint}
}

func fallbackProtocolPlan(endpoint string) (protocolPlan, bool) {
	switch endpoint {
	case chatCompletionsEndpoint:
		return protocolPlan{clientEndpoint: endpoint, upstreamEndpoint: responsesEndpoint, conversion: chatToResponsesConversion}, true
	case responsesEndpoint:
		return protocolPlan{clientEndpoint: endpoint, upstreamEndpoint: chatCompletionsEndpoint, conversion: responsesToChatConversion}, true
	default:
		return protocolPlan{}, false
	}
}

func (p protocolPlan) converts() bool {
	return p.conversion != ""
}

func (p protocolPlan) convertRequest(body []byte) ([]byte, error) {
	switch p.conversion {
	case "":
		return body, nil
	case chatToResponsesConversion:
		return chatRequestToResponses(body)
	case responsesToChatConversion:
		return responsesRequestToChat(body)
	default:
		return nil, gerror.New("unsupported protocol conversion")
	}
}

func (p protocolPlan) convertResponse(body []byte) []byte {
	if !p.converts() || !json.Valid(body) || gjson.GetBytes(body, "error").Exists() {
		return body
	}
	switch p.conversion {
	case chatToResponsesConversion:
		return responsesResponseToChat(body)
	case responsesToChatConversion:
		return chatResponseToResponses(body)
	default:
		return body
	}
}

func shouldFallbackWithProtocolConversion(status int, body []byte) bool {
	switch status {
	case 404, 405, 501:
		return true
	case 400, 422:
		message := strings.ToLower(gjson.GetBytes(body, "error.message").String())
		if message == "" {
			message = strings.ToLower(string(body))
		}
		for _, marker := range []string{"endpoint", "chat completions", "responses api", "responses endpoint", "not support", "unsupported", "not compatible"} {
			if strings.Contains(message, marker) {
				return true
			}
		}
	}
	return false
}

func chatRequestToResponses(body []byte) ([]byte, error) {
	source, err := decodeProtocolObject(body)
	if err != nil {
		return nil, err
	}
	target := copyProtocolFields(source, "model", "stream", "user", "temperature", "top_p", "parallel_tool_calls", "metadata")
	if value, exists := source["max_completion_tokens"]; exists {
		target["max_output_tokens"] = value
	} else if value, exists := source["max_tokens"]; exists {
		target["max_output_tokens"] = value
	}
	if value, exists := source["tools"]; exists {
		target["tools"] = chatToolsToResponses(value)
	}
	if value, exists := source["tool_choice"]; exists {
		target["tool_choice"] = chatToolChoiceToResponses(value)
	}
	if value, exists := source["response_format"]; exists {
		target["text"] = map[string]any{"format": value}
	}

	var (
		instructions []string
		input        []any
	)
	for _, value := range arrayValue(source["messages"]) {
		message, ok := objectValue(value)
		if !ok {
			continue
		}
		role := stringValue(message["role"])
		content := message["content"]
		switch role {
		case "system", "developer":
			if text := protocolText(content); text != "" {
				instructions = append(instructions, text)
				continue
			}
		case "tool":
			input = append(input, map[string]any{"type": "function_call_output", "call_id": stringValue(message["tool_call_id"]), "output": content})
			continue
		}
		if role == "" {
			role = "user"
		}
		input = append(input, map[string]any{"role": role, "content": chatContentToResponses(content)})
	}
	if len(instructions) > 0 {
		target["instructions"] = strings.Join(instructions, "\n\n")
	}
	if len(input) > 0 {
		target["input"] = input
	}
	return encodeProtocolObject(target)
}

func responsesRequestToChat(body []byte) ([]byte, error) {
	source, err := decodeProtocolObject(body)
	if err != nil {
		return nil, err
	}
	target := copyProtocolFields(source, "model", "stream", "user", "temperature", "top_p", "parallel_tool_calls")
	if value, exists := source["max_output_tokens"]; exists {
		target["max_completion_tokens"] = value
	}
	if value, exists := source["tools"]; exists {
		target["tools"] = responsesToolsToChat(value)
	}
	if value, exists := source["tool_choice"]; exists {
		target["tool_choice"] = responsesToolChoiceToChat(value)
	}
	if text, ok := objectValue(source["text"]); ok {
		if format, exists := text["format"]; exists {
			target["response_format"] = format
		}
	}
	messages := make([]any, 0)
	if instructions := stringValue(source["instructions"]); instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	messages = append(messages, responsesInputToChat(source["input"])...)
	if len(messages) > 0 {
		target["messages"] = messages
	}
	return encodeProtocolObject(target)
}

func decodeProtocolObject(body []byte) (map[string]any, error) {
	payload := make(map[string]any)
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, gerror.Wrap(err, "decode protocol payload")
	}
	return payload, nil
}

func encodeProtocolObject(payload map[string]any) ([]byte, error) {
	result, err := json.Marshal(payload)
	return result, gerror.Wrap(err, "encode protocol payload")
}

func copyProtocolFields(source map[string]any, fields ...string) map[string]any {
	target := make(map[string]any, len(fields))
	for _, field := range fields {
		if value, exists := source[field]; exists {
			target[field] = value
		}
	}
	return target
}

func arrayValue(value any) []any {
	items, _ := value.([]any)
	return items
}

func objectValue(value any) (map[string]any, bool) {
	item, ok := value.(map[string]any)
	return item, ok
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringOr(value any, fallback string) string {
	if text := stringValue(value); text != "" {
		return text
	}
	return fallback
}

func protocolText(value any) string {
	if text := stringValue(value); text != "" {
		return text
	}
	var text strings.Builder
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if ok {
			text.WriteString(stringValue(item["text"]))
		}
	}
	return text.String()
}
