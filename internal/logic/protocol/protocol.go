package protocol

import (
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
)

const (
	// ChatCompletionsEndpoint 和 ResponsesEndpoint 是 Relay 路由及协议转换共用的
	// 上游端点标识。转换规则只在这两个 OpenAI 兼容端点之间生效。
	ChatCompletionsEndpoint = "/chat/completions"
	ResponsesEndpoint       = "/responses"

	chatToResponsesConversion = "chat_to_responses"
	responsesToChatConversion = "responses_to_chat"
)

type Plan struct {
	clientEndpoint   string
	upstreamEndpoint string
	conversion       string
}

func directPlan(endpoint string) Plan {
	return Plan{clientEndpoint: endpoint, upstreamEndpoint: endpoint}
}

func PreferredPlan(endpoint, upstreamModel string) Plan {
	isGPT := isGPTModel(upstreamModel)
	if (endpoint == ChatCompletionsEndpoint && isGPT) || (endpoint == ResponsesEndpoint && !isGPT) {
		if plan, ok := fallbackPlan(endpoint); ok {
			return plan
		}
	}
	return directPlan(endpoint)
}

func AlternatePlan(endpoint string, primary Plan) (Plan, bool) {
	if primary.Converts() {
		return directPlan(endpoint), true
	}
	return fallbackPlan(endpoint)
}

func isGPTModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-")
}

func fallbackPlan(endpoint string) (Plan, bool) {
	switch endpoint {
	case ChatCompletionsEndpoint:
		return Plan{clientEndpoint: endpoint, upstreamEndpoint: ResponsesEndpoint, conversion: chatToResponsesConversion}, true
	case ResponsesEndpoint:
		return Plan{clientEndpoint: endpoint, upstreamEndpoint: ChatCompletionsEndpoint, conversion: responsesToChatConversion}, true
	default:
		return Plan{}, false
	}
}

func (p Plan) ClientEndpoint() string {
	return p.clientEndpoint
}

func (p Plan) UpstreamEndpoint() string {
	return p.upstreamEndpoint
}

func (p Plan) Conversion() string {
	return p.conversion
}

func (p Plan) Converts() bool {
	return p.conversion != ""
}

func (p Plan) ConvertRequest(body []byte) ([]byte, error) {
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

func (p Plan) ConvertResponse(body []byte) []byte {
	if !p.Converts() || !json.Valid(body) || gjson.GetBytes(body, "error").Exists() {
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

func ShouldFallback(status int, body []byte) bool {
	switch status {
	case 404, 405, 501:
		return true
	case 400, 422:
		message := strings.ToLower(gjson.GetBytes(body, "error.message").String())
		if message == "" {
			message = strings.ToLower(string(body))
		}
		// 文件 URL/文件数据错误属于请求内容问题，不是协议端点不支持。
		// 若把它当成协议回退条件，会把同一个失效 URL 再发给 Chat 上游，
		// 既不能修复请求，还会造成额外重试。
		if strings.Contains(message, "error while downloading file") || strings.Contains(message, "downloading file") {
			return false
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
	target := copyProtocolFields(source, "model", "stream", "user", "temperature", "top_p", "parallel_tool_calls", "metadata", "prompt_cache_key", "prompt_cache_options", "prompt_cache_retention")
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
			// 工具结果可能携带上一次 Responses 输出的嵌套 output 数组。
			// 其中的 Chat image_url 也必须递归转换，否则会落到
			// input[n].output[0].type=image_url 的上游校验错误。
			input = append(input, map[string]any{"type": "function_call_output", "call_id": stringValue(message["tool_call_id"]), "output": normalizeNestedChatContent(content)})
			continue
		case "assistant":
			assistantContent := chatAssistantContentToResponses(content)
			if refusal := stringValue(message["refusal"]); refusal != "" {
				assistantContent = append(assistantContent, map[string]any{"type": "refusal", "refusal": refusal})
			}
			if toolCalls := chatToolCallsToResponses(message["tool_calls"]); len(toolCalls) > 0 {
				if len(assistantContent) > 0 {
					input = append(input, map[string]any{"role": role, "content": assistantContent})
				}
				input = append(input, toolCalls...)
				continue
			}
			input = append(input, map[string]any{"role": role, "content": assistantContent})
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
	target := copyProtocolFields(source, "model", "stream", "user", "temperature", "top_p", "parallel_tool_calls", "prompt_cache_key", "prompt_cache_options", "prompt_cache_retention")
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
