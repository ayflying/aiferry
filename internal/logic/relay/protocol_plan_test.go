package relay

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/protocol"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

// commandCodeTypeConfig 是 Command Code 渠道类型的等价配置：
// 只提供 Chat Completions 端点，因此声明 chatCompletionsOnly。
const commandCodeTypeConfig = `{
  "baseUrl": "https://api.commandcode.ai/provider/v1",
  "models": {"method": "GET", "path": "/models", "listPath": "data", "idPath": "id", "authType": "channel_key", "headerName": "Authorization", "headerPrefix": "Bearer "},
  "costs": {"adapter": "none"},
  "pricing": {"adapter": "none"},
  "protocol": {"chatCompletionsOnly": true}
}`

func relayWithBuiltinType(t *testing.T, code, typeConfig string) *sRelay {
	t.Helper()
	registry := &config.BuiltinRegistry{ChannelTypes: []config.BuiltinChannelType{{
		ID: 1, Name: code, Code: code, Config: json.RawMessage(typeConfig),
	}}}
	return &sRelay{types: channeltype.New(registry, nil)}
}

// Command Code 上游对 GPT 系模型也只提供 /chat/completions，若沿用
// 「gpt-* 优先转投 /responses」的推断，每次请求都会先失败再回退。
func TestChatCompletionsOnlyChannelPinsChatForGPTModels(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{}

	service := relayWithBuiltinType(t, "commandcode", commandCodeTypeConfig)
	candidate := Candidate{ChannelType: "commandcode", UpstreamName: "gpt-5.6-luna"}
	plan := service.preferredProtocolPlan(context.Background(), protocol.ChatCompletionsEndpoint, candidate, true, settings)
	if plan.UpstreamEndpoint() != protocol.ChatCompletionsEndpoint || plan.Conversion() != "" {
		t.Fatalf("plan = %+v, want direct Chat Completions", plan)
	}
}

// Responses 客户端访问这类上游时转换为 Chat，仍然落在唯一可用的端点上。
func TestChatCompletionsOnlyChannelConvertsResponsesClient(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{}

	service := relayWithBuiltinType(t, "commandcode", commandCodeTypeConfig)
	candidate := Candidate{ChannelType: "commandcode", UpstreamName: "deepseek/deepseek-v4-flash"}
	plan := service.preferredProtocolPlan(context.Background(), protocol.ResponsesEndpoint, candidate, true, settings)
	if plan.UpstreamEndpoint() != protocol.ChatCompletionsEndpoint || plan.Conversion() != "responses_to_chat" {
		t.Fatalf("plan = %+v, want Responses to Chat conversion", plan)
	}
}

// 未声明协议偏好的渠道保持原行为：gpt-* 仍然优先走上游 /responses。
func TestChannelWithoutProtocolPreferenceKeepsGPTResponsesPlan(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{}

	service := &sRelay{}
	candidate := Candidate{ChannelType: "openai", UpstreamName: "gpt-5.6-luna"}
	plan := service.preferredProtocolPlan(context.Background(), protocol.ChatCompletionsEndpoint, candidate, true, settings)
	if plan.UpstreamEndpoint() != protocol.ResponsesEndpoint || plan.Conversion() != "chat_to_responses" {
		t.Fatalf("plan = %+v, want Chat to Responses conversion", plan)
	}
}

// 关闭协议转换后，gpt-* 模型也直连客户端声明的端点，不再转投上游 /responses。
func TestProtocolConversionDisabledPinsClientEndpoint(t *testing.T) {
	settings := adminapi.SystemResilienceSettingsInput{}

	service := &sRelay{}
	candidate := Candidate{ChannelType: "openai", UpstreamName: "gpt-5.6-luna"}
	plan := service.preferredProtocolPlan(context.Background(), protocol.ChatCompletionsEndpoint, candidate, false, settings)
	if plan.Converts() || plan.UpstreamEndpoint() != protocol.ChatCompletionsEndpoint {
		t.Fatalf("plan = %+v, want direct Chat Completions", plan)
	}
}
