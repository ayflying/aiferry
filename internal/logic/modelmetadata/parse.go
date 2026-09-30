package modelmetadata

import (
	"encoding/json"
	"github.com/tidwall/gjson"
	"strconv"
)

// Parse reads only explicitly declared, type-checked facts. Model names never imply support.
func Parse(item gjson.Result) Metadata {
	var m Metadata
	m.DisplayName = text(item, "display_name", "name")
	m.Description = text(item, "description")
	m.ContextLength = integer(item, "context_length", "architecture.context_length")
	m.MaxOutputTokens = integer(item, "max_output_tokens", "top_provider.max_completion_tokens", "architecture.max_output_tokens")
	m.InputModalities = modalities(item, "input_modalities", "architecture.input_modalities")
	m.OutputModalities = modalities(item, "output_modalities", "architecture.output_modalities")
	m.Tools = boolean(item, "tools", "capabilities.tools", "capabilities.tool_calling")
	m.Reasoning = boolean(item, "reasoning", "capabilities.reasoning")
	m.StructuredOutput = boolean(item, "structured_output", "capabilities.structured_output", "capabilities.structured_outputs")
	return m
}
func text(item gjson.Result, paths ...string) *string {
	for _, path := range paths {
		v := item.Get(path)
		if v.Type == gjson.String {
			s := v.String()
			limit := 8192
			if path != "description" {
				limit = 191
			}
			if len([]rune(s)) <= limit {
				return &s
			}
		}
	}
	return nil
}
func integer(item gjson.Result, paths ...string) *int64 {
	for _, path := range paths {
		v := item.Get(path)
		if v.Type != gjson.Number {
			continue
		}
		n, err := strconv.ParseInt(v.Raw, 10, 64)
		if err == nil && n > 0 && n <= 1<<53-1 {
			return &n
		}
	}
	return nil
}
func boolean(item gjson.Result, paths ...string) *bool {
	for _, path := range paths {
		v := item.Get(path)
		if v.Type == gjson.True || v.Type == gjson.False {
			b := v.Bool()
			return &b
		}
	}
	return nil
}
func modalities(item gjson.Result, paths ...string) []string {
	for _, path := range paths {
		v := item.Get(path)
		if !v.IsArray() {
			continue
		}
		var values []string
		if err := json.Unmarshal([]byte(v.Raw), &values); err != nil {
			continue
		}
		if Validate(Metadata{InputModalities: values}) == nil {
			return values
		}
	}
	return nil
}
