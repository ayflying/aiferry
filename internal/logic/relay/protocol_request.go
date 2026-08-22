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

func chatToolCallsToResponses(value any) []any {
	result := make([]any, 0)
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok || stringValue(item["type"]) != "function" {
			continue
		}
		function, ok := objectValue(item["function"])
		if !ok {
			continue
		}
		callID := stringValue(item["id"])
		result = append(result, map[string]any{
			"type":      "function_call",
			"id":        callID,
			"call_id":   callID,
			"name":      stringValue(function["name"]),
			"arguments": stringValue(function["arguments"]),
			"status":    "completed",
		})
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
			converted := map[string]any{"type": "input_text", "text": stringValue(item["text"])}
			copyPromptCacheBreakpoint(item, converted)
			parts = append(parts, converted)
		case "image_url":
			// Chat Completions 使用 {"image_url":{"url":"...","detail":"..."}}，而 Responses
			// 的 input_image.image_url 必须是字符串。兼容已是字符串的扩展客户端，
			// 但绝不能把 Chat 的整个对象直接转发给 Responses。
			imageURL := item["image_url"]
			converted := map[string]any{"type": "input_image"}
			if image, ok := objectValue(imageURL); ok {
				converted["image_url"] = stringValue(image["url"])
				if detail := stringValue(image["detail"]); detail != "" {
					converted["detail"] = detail
				}
			} else {
				converted["image_url"] = imageURL
			}
			copyPromptCacheBreakpoint(item, converted)
			parts = append(parts, converted)
		case "file":
			// 两种协议的文件元数据层级不同：Chat 放在 file 对象中，Responses
			// 要求 file_id、file_data、filename 直接位于 input_file 内容块。
			// Chat 的 file.url 不能直接转发：Responses 会主动下载该 URL，
			// URL 失效或不可公网访问时就会返回 400 invalid_value/404。
			converted := copyProtocolFields(item, "file_id", "file_data", "filename")
			if file, ok := objectValue(item["file"]); ok {
				for _, field := range []string{"file_id", "file_data", "filename"} {
					if value, exists := file[field]; exists {
						converted[field] = value
					}
				}
			}
			converted["type"] = "input_file"
			copyPromptCacheBreakpoint(item, converted)
			parts = append(parts, converted)
		default:
			// input_audio 等两种协议字段完全一致的内容块无需改写，原样保留。
			parts = append(parts, item)
		}
	}
	return parts
}

func chatAssistantContentToResponses(value any) []any {
	result := make([]any, 0)
	if text, ok := value.(string); ok {
		return append(result, map[string]any{"type": "output_text", "text": text})
	}
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		switch stringValue(item["type"]) {
		case "text", "input_text", "output_text":
			converted := map[string]any{"type": "output_text", "text": stringValue(item["text"])}
			copyPromptCacheBreakpoint(item, converted)
			result = append(result, converted)
		case "refusal":
			refusal := stringOr(item["refusal"], stringValue(item["text"]))
			if refusal != "" {
				result = append(result, map[string]any{"type": "refusal", "refusal": refusal})
			}
		}
	}
	return result
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
			converted := map[string]any{"type": "text", "text": stringValue(item["text"])}
			copyPromptCacheBreakpoint(item, converted)
			parts = append(parts, converted)
		case "input_image":
			// Responses 使用字符串 image_url；转换回 Chat 时重新包成 image_url 对象，
			// 避免把 Responses 专用结构发送到严格校验的 Chat 上游。
			image := map[string]any{"url": item["image_url"]}
			if detail := stringValue(item["detail"]); detail != "" {
				image["detail"] = detail
			}
			converted := map[string]any{"type": "image_url", "image_url": image}
			copyPromptCacheBreakpoint(item, converted)
			parts = append(parts, converted)
		case "input_file":
			// 与正向转换相反，将 Responses 的扁平文件字段归入 Chat 的 file 对象。
			file := copyProtocolFields(item, "file_id", "file_data", "filename")
			converted := map[string]any{"type": "file", "file": file}
			copyPromptCacheBreakpoint(item, converted)
			parts = append(parts, converted)
		default:
			// input_audio 等同构字段保持原样，避免丢失格式或 Base64 数据。
			parts = append(parts, item)
		}
	}
	return parts
}

func copyPromptCacheBreakpoint(source, target map[string]any) {
	if value, exists := source["prompt_cache_breakpoint"]; exists {
		target["prompt_cache_breakpoint"] = value
	}
}
