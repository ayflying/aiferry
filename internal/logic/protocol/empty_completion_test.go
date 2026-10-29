package protocol

import "testing"

// EmptyCompletion 只在协议转换方向生效，并且判定读的是**上游原始响应**：
// 只有正文、refusal 与工具调用三者都为空的响应才算空完成，正常但内容很少的
// 回答不能被误判成失败。
func TestEmptyCompletionDetection(t *testing.T) {
	responsesPlan := PreferredChatCompletionsPlan(ResponsesEndpoint)
	chatPlan := PreferredResponsesPlan(ChatCompletionsEndpoint)

	cases := []struct {
		name string
		plan Plan
		body string
		want bool
	}{
		{
			name: "chat choices empty", plan: responsesPlan,
			body: `{"id":"","model":"","choices":[]}`, want: true,
		},
		{
			name: "chat choices missing entirely", plan: responsesPlan,
			body: `{}`, want: true,
		},
		{
			name: "chat message missing", plan: responsesPlan,
			body: `{"choices":[{"index":0}]}`, want: true,
		},
		{
			name: "chat empty message", plan: responsesPlan,
			body: `{"choices":[{"index":0,"message":{"role":"assistant","content":""}}]}`, want: true,
		},
		{
			name: "chat null content without tools", plan: responsesPlan,
			body: `{"choices":[{"index":0,"message":{"role":"assistant","content":null}}]}`, want: true,
		},
		{
			name: "chat with content", plan: responsesPlan,
			body: `{"choices":[{"index":0,"message":{"role":"assistant","content":"你好"}}]}`, want: false,
		},
		{
			name: "chat with tool calls", plan: responsesPlan,
			body: `{"choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"pwsh","arguments":"{}"}}]}}]}`, want: false,
		},
		{
			name: "chat with refusal", plan: responsesPlan,
			body: `{"choices":[{"index":0,"message":{"role":"assistant","content":"","refusal":"不能这样做"}}]}`, want: false,
		},
		{
			name: "upstream error is not empty completion", plan: responsesPlan,
			body: `{"error":{"message":"上游失败","type":"upstream_error"}}`, want: false,
		},
		{
			name: "responses empty output", plan: chatPlan,
			body: `{"object":"response","status":"completed","output":[]}`, want: true,
		},
		{
			name: "responses with message", plan: chatPlan,
			body: `{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"好"}]}]}`, want: false,
		},
		{
			name: "responses without output field is unknown shape", plan: chatPlan,
			body: `{"object":"response","status":"completed"}`, want: false,
		},
		{
			// 直连不做判定：上游自己负责自己的响应形态。
			name: "direct plan never flags", plan: DirectPlan(ResponsesEndpoint),
			body: `{"object":"response","status":"completed","output":[]}`, want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.EmptyCompletion([]byte(tc.body)); got != tc.want {
				t.Fatalf("EmptyCompletion = %v, want %v (plan=%+v)", got, tc.want, tc.plan)
			}
		})
	}
}
