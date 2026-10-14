package channel

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/gogf/gf/v2/errors/gerror"
)

// openCodeCoreTools 是免费层智能体会话要求的工具名单（OpenCode 2.x 拼写）。
// 缺少 shell 或 read 的请求会被 403 FreeTierError 拒绝。
//
// "shell" 是 OpenCode 2.x 的拼写；2.x 之前的 "bash" 不注入——免费层会
// 校验它识别的每个工具定义，网关自己拼的 "bash" 定义与上游期望的
// 不一致（2026-10-14 实测，与 opencode2api 同源结论）。
var openCodeCoreTools = []string{"shell", "edit", "glob", "grep", "read"}

// opencodeToolsJSON 是从官方客户端抓取的真实工具定义（描述与 JSON
// schema）。免费层会校验它识别的每个工具名对应的定义，网关合成的桩
// 即使名字正确也会 403，因此内置真实定义，同时用于补齐缺失的核心
// 工具与改写下游声明了同名工具的请求。
//
//go:embed opencode_tools.json
var opencodeToolsJSON []byte

type opencodeToolDefinition struct {
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

var (
	opencodeToolsOnce sync.Once
	opencodeTools     map[string]opencodeToolDefinition
)

// knownOpencodeTool 返回官方真实工具定义；JSON 损坏时退化为空表，
// 调用方据此回退到最小形状（不阻塞转发）。
func knownOpencodeTool(name string) (opencodeToolDefinition, bool) {
	opencodeToolsOnce.Do(func() {
		definitions := map[string]opencodeToolDefinition{}
		if err := json.Unmarshal(opencodeToolsJSON, &definitions); err != nil {
			definitions = map[string]opencodeToolDefinition{}
		}
		opencodeTools = definitions
	})
	definition, ok := opencodeTools[name]
	return definition, ok
}

// openCodeFreeTool 按官方真实定义渲染一个核心工具；定义未知时退化为
// 最小形状（描述仅占位、空 schema）。
func openCodeFreeTool(name string) map[string]any {
	description := "Agent tool " + name
	parameters := map[string]any{"type": "object", "properties": map[string]any{}}
	if definition, ok := knownOpencodeTool(name); ok {
		description = definition.Description
		if definition.Parameters != nil {
			parameters = definition.Parameters
		}
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": description,
			"parameters":  parameters,
		},
	}
}

// ApplyOpenCodeFreeBody 为免费层 chat completions 请求改写请求体：
// 强制 stream=true、按官方门禁重写工具名单（上游识别的每个工具名必须
// 携带官方真实定义，桩定义即 403；缺失的核心工具补齐；上游不认识的
// 名字原样保留，下游函数调用不受影响）、剥离 tool_choice:none（实测
// 该取值会被 2026-10-14 门禁以 FreeTierError 拒绝，交给上游默认 auto；
// 其余取值原样保留维持下游语义）、剥离缓存控制字段，避免严格校验的
// 上游报 UNKNOWN_FIELD。返回值 forcedStream 表示是否把客户端的非流式
// 请求改成了流式，调用方据此对回包做 SSE 聚合。
func ApplyOpenCodeFreeBody(body []byte) (result []byte, forcedStream bool, err error) {
	var payload map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, false, gerror.Wrap(err, "decode free lane body")
	}
	originalStream, _ := payload["stream"].(bool)
	removePromptCacheControls(payload)
	payload["stream"] = true
	forcedStream = !originalStream

	tools, _ := payload["tools"].([]any)
	present := make(map[string]bool, len(tools))
	for index, item := range tools {
		name := openCodeToolName(item)
		if name == "" {
			continue
		}
		present[name] = true
		if _, known := knownOpencodeTool(name); known {
			// 下游声明了官方工具同名工具时改写为真实定义：桩定义会被
			// 门禁拒绝，同名异定义的下游工具否则会连累整个请求。
			tools[index] = openCodeFreeTool(name)
		}
	}
	for _, name := range openCodeCoreTools {
		if !present[name] {
			tools = append(tools, openCodeFreeTool(name))
		}
	}
	payload["tools"] = tools

	if choice, _ := payload["tool_choice"].(string); choice == "none" {
		delete(payload, "tool_choice")
	}

	options, _ := payload["stream_options"].(map[string]any)
	if options == nil {
		options = make(map[string]any)
		payload["stream_options"] = options
	}
	options["include_usage"] = true

	result, err = json.Marshal(payload)
	return result, forcedStream, gerror.Wrap(err, "encode free lane body")
}

// openCodeToolName 从工具对象里取出函数名，兼容 OpenAI（function.name）
// 与 Anthropic（name）两种形态。
func openCodeToolName(item any) string {
	object, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	if function, ok := object["function"].(map[string]any); ok {
		name, _ := function["name"].(string)
		return name
	}
	name, _ := object["name"].(string)
	return name
}
