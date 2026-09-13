package protocol

import "strings"

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

// responsesInputToChat 把 Responses 的 input 数组还原成 Chat 的 messages。
//
// Responses 把「一次工具调用」拆成独立的 function_call 输入项，而 Chat 要求它出现在 assistant
// 消息的 tool_calls 里，因此这里需要缓冲相邻的 function_call（并行调用是多个相邻项）后合并成
// 一条 assistant 消息；紧随其后的 function_call_output 再落成 role=tool 消息，两者的 call_id
// 必须一一对应，否则上游会因为「工具结果找不到对应的工具调用」而拒绝请求。
//
// 同一段里出现的 reasoning 输入项（思考模式的历史）会作为 reasoning_content 挂到这条
// assistant 消息上——DeepSeek/Kimi 等上游要求发生过工具调用后必须原样回传思考内容。
func responsesInputToChat(value any) []any {
	if text, ok := value.(string); ok {
		return []any{map[string]any{"role": "user", "content": text}}
	}
	result := make([]any, 0)
	var (
		pendingAssistant map[string]any
		pendingToolCalls []any
		pendingReasoning string
	)
	flush := func() {
		if len(pendingToolCalls) > 0 {
			message := map[string]any{"role": "assistant", "content": nil, "tool_calls": pendingToolCalls}
			if pendingAssistant != nil {
				message["content"] = pendingAssistant["content"]
			}
			if pendingReasoning != "" {
				message["reasoning_content"] = pendingReasoning
			}
			result = append(result, message)
		} else if pendingAssistant != nil {
			// 没有工具调用时思考内容不参与校验，丢弃以免上游拒绝未知字段。
			result = append(result, pendingAssistant)
		}
		pendingAssistant, pendingToolCalls, pendingReasoning = nil, nil, ""
	}
	for _, itemValue := range arrayValue(value) {
		item, ok := objectValue(itemValue)
		if !ok {
			continue
		}
		switch itemType := stringValue(item["type"]); itemType {
		case "function_call":
			// Responses 的 id 是条目 id，call_id 才是与 function_call_output 对应的键；
			// 回退到 id 兼容只填了 id 的客户端。
			callID := stringOr(item["call_id"], stringValue(item["id"]))
			pendingToolCalls = append(pendingToolCalls, map[string]any{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name": stringValue(item["name"]),
					// Chat 上游要求 arguments 是字符串，缺失时给空对象。
					"arguments": stringOr(item["arguments"], "{}"),
				},
			})
		case "reasoning":
			if text := responsesReasoningText(item); text != "" {
				pendingReasoning = text
			}
		case "function_call_output":
			flush()
			result = append(result, map[string]any{"role": "tool", "tool_call_id": stringValue(item["call_id"]), "content": item["output"]})
		default:
			if itemType != "" && itemType != "message" {
				// 未知输入项（local_shell_call、computer_call 等）没有 Chat 对应形态，
				// 直接跳过；降级成 role=user/content=null 会向对话里注入空用户消息。
				continue
			}
			role := stringOr(item["role"], "user")
			if role == "assistant" {
				// 缓冲 assistant 文本，等待可能紧随其后的 function_call 合并成同一条消息。
				// 只有确实存在待发出的内容时才 flush，否则会丢掉刚缓冲的 reasoning
				// （Responses 的顺序是 reasoning -> assistant 文本 -> function_call）。
				if len(pendingToolCalls) > 0 || pendingAssistant != nil {
					flush()
				}
				pendingAssistant = map[string]any{"role": role, "content": responsesContentToChat(item["content"])}
				continue
			}
			flush()
			result = append(result, map[string]any{"role": role, "content": responsesContentToChat(item["content"])})
		}
	}
	flush()
	return result
}

// responsesReasoningText 提取 Responses reasoning 输入项里的思考文本。
// summary 是 Responses 的公开摘要，content 是完整思考内容的回传形态，两者都可能出现。
func responsesReasoningText(item map[string]any) string {
	var text strings.Builder
	for _, field := range []string{"summary", "content"} {
		for _, partValue := range arrayValue(item[field]) {
			part, ok := objectValue(partValue)
			if !ok {
				continue
			}
			text.WriteString(stringValue(part["text"]))
		}
	}
	if text.Len() > 0 {
		return text.String()
	}
	return stringValue(item["text"])
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
		case "image_url":
			// assistant 历史消息也可能包含图片。虽然它来自 assistant，Responses
			// 仍会严格校验内容块类型，不能把 Chat 的 image_url 原样传入。
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
			result = append(result, converted)
		case "file":
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
			result = append(result, converted)
		case "input_audio":
			// input_audio 已是 Responses 支持的类型，保留其 Base64 数据和格式。
			result = append(result, item)
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

// normalizeNestedChatContent 递归整理工具结果等嵌套历史内容。
// 这类内容不一定经过普通 message.content 分支，但其中仍可能存在
// Chat 格式的 image_url；Responses 会校验嵌套 output 数组中的每个 type。
func normalizeNestedChatContent(value any) any {
	if items, ok := value.([]any); ok {
		result := make([]any, len(items))
		for i, item := range items {
			result[i] = normalizeNestedChatContent(item)
		}
		return result
	}
	item, ok := objectValue(value)
	if !ok {
		return value
	}
	if stringValue(item["type"]) == "image_url" {
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
		return converted
	}
	result := make(map[string]any, len(item))
	for key, child := range item {
		result[key] = normalizeNestedChatContent(child)
	}
	return result
}

func copyPromptCacheBreakpoint(source, target map[string]any) {
	if value, exists := source["prompt_cache_breakpoint"]; exists {
		target["prompt_cache_breakpoint"] = value
	}
}
