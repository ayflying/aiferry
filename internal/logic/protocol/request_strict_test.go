package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tidwall/gjson"
)

// 目标工具的互斥字段必须保持可选，不能因协议默认值差异强制模型填满。
func TestChatToolsToResponsesStrictSemantics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		strict  any
		present bool
		want    any
	}{
		{name: "omitted", want: false},
		{name: "explicit_false", strict: false, present: true, want: false},
		{name: "explicit_true", strict: true, present: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parameters := map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":          map[string]any{"type": "string"},
					"goal_id":         map[string]any{"type": "string"},
					"revision":        map[string]any{"type": "integer"},
					"objective":       map[string]any{"type": "string"},
					"max_goal_rounds": map[string]any{"type": "integer"},
					"blocked_reason":  map[string]any{"type": "string"},
				},
				"required": []any{"action", "goal_id", "revision"},
			}
			function := map[string]any{"name": "update_goal", "parameters": parameters}
			if tc.present {
				function["strict"] = tc.strict
			}
			input, err := json.Marshal(map[string]any{
				"model": "gpt-test", "messages": []any{},
				"tools": []any{map[string]any{"type": "function", "function": function}},
			})
			if err != nil {
				t.Fatal(err)
			}
			body, err := chatRequestToResponses(input)
			if err != nil {
				t.Fatal(err)
			}
			strict := gjson.GetBytes(body, "tools.0.strict")
			if !strict.Exists() || strict.Value() != tc.want {
				t.Fatalf("strict = %v, want %v: %s", strict.Value(), tc.want, body)
			}
			var actual map[string]any
			if err := json.Unmarshal([]byte(gjson.GetBytes(body, "tools.0.parameters").Raw), &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, parameters) {
				t.Fatalf("parameters changed: %s", body)
			}
			if _, exists := function["strict"]; exists != tc.present {
				t.Fatal("source function mutated")
			}
		})
	}
}

func TestChatToolsToResponsesKeepsNonFunctionTools(t *testing.T) {
	source := map[string]any{"type": "custom", "name": "code_exec"}
	got := chatToolsToResponses([]any{source})
	if len(got) != 1 || !reflect.DeepEqual(got[0], source) {
		t.Fatalf("non-function tool changed: %#v", got)
	}
}
