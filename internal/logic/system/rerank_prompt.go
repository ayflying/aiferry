package system

import "encoding/json"

// rerankPromptTexts 同时检查查询与候选文档。对象文档可含 title/text 等
// 上游自定义字段，递归读取字符串值，避免对象形式绕过敏感词规则。
func rerankPromptTexts(payload map[string]json.RawMessage) []string {
	var texts []string
	var collect func(any)
	collect = func(value any) {
		switch value := value.(type) {
		case string:
			texts = append(texts, value)
		case []any:
			for _, item := range value {
				collect(item)
			}
		case map[string]any:
			for _, item := range value {
				collect(item)
			}
		}
	}
	for _, field := range []string{"query", "documents"} {
		var value any
		if json.Unmarshal(payload[field], &value) == nil {
			collect(value)
		}
	}
	return texts
}
