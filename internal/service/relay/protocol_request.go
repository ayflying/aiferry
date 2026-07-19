package relay

func chatToolsToResponses(value any) []any {
	result := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		if stringValue(item["type"]) != "function" {
			result = append(result, item)
			continue
		}
		function, ok := objectValue(item["function"])
		if !ok {
			continue
		}
		converted := copyProtocolFields(function, "name", "description", "parameters", "strict")
		converted["type"] = "function"
		result = append(result, converted)
	}
	return result
}

func responsesToolsToChat(value any) []any {
	result := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		if stringValue(item["type"]) != "function" {
			continue
		}
		function := copyProtocolFields(item, "name", "description", "parameters", "strict")
		result = append(result, map[string]any{"type": "function", "function": function})
	}
	return result
}

func chatToolChoiceToResponses(value any) any {
	choice, ok := objectValue(value)
	if !ok {
		return value
	}
	function, ok := objectValue(choice["function"])
	if !ok {
		return choice
	}
	return map[string]any{"type": "function", "name": stringValue(function["name"])}
}

func responsesToolChoiceToChat(value any) any {
	choice, ok := objectValue(value)
	if !ok || stringValue(choice["type"]) != "function" {
		return value
	}
	return map[string]any{"type": "function", "function": map[string]any{"name": stringValue(choice["name"])}}
}

func responsesInputToChat(value any) []any {
	if text, ok := value.(string); ok {
		return []any{map[string]any{"role": "user", "content": text}}
	}
	result := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		switch stringValue(item["type"]) {
		case "function_call_output":
			result = append(result, map[string]any{"role": "tool", "tool_call_id": stringValue(item["call_id"]), "content": item["output"]})
		default:
			role := stringOr(item["role"], "user")
			result = append(result, map[string]any{"role": role, "content": responsesContentToChat(item["content"])})
		}
	}
	return result
}

func chatContentToResponses(value any) any {
	if _, ok := value.(string); ok {
		return value
	}
	parts := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		switch stringValue(item["type"]) {
		case "text":
			parts = append(parts, map[string]any{"type": "input_text", "text": stringValue(item["text"])})
		case "image_url":
			parts = append(parts, map[string]any{"type": "input_image", "image_url": item["image_url"]})
		default:
			parts = append(parts, item)
		}
	}
	return parts
}

func responsesContentToChat(value any) any {
	if _, ok := value.(string); ok {
		return value
	}
	parts := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		switch stringValue(item["type"]) {
		case "input_text", "output_text":
			parts = append(parts, map[string]any{"type": "text", "text": stringValue(item["text"])})
		case "input_image":
			parts = append(parts, map[string]any{"type": "image_url", "image_url": item["image_url"]})
		default:
			parts = append(parts, item)
		}
	}
	return parts
}
