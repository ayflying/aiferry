package relay

import (
	"net/http"
	"testing"
)

func TestConversationKeyPrefersSessionHeader(t *testing.T) {
	incoming := http.Header{}
	incoming.Set("Session_id", "sesn_abc123")
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"你好"}]}`)
	first := conversationKeyForRequest(incoming, body, 7)
	if first == "" {
		t.Fatal("带会话头时必须得到会话标识")
	}
	// 同一会话标识 + 不同首条消息仍然是同一段对话：标识只由会话头决定。
	otherBody := []byte(`{"model":"m","messages":[{"role":"user","content":"换一个问题"}]}`)
	if conversationKeyForRequest(incoming, otherBody, 7) != first {
		t.Fatal("同一会话头的两次请求必须得到同一个会话标识")
	}
	// 不同 API Key 不应影响由会话头得出的标识（会话头本身就唯一）。
	if conversationKeyForRequest(incoming, body, 99) != first {
		t.Fatal("会话头优先时不应掺入 API Key")
	}
}

func TestConversationKeyUsesMetadataUserID(t *testing.T) {
	body := []byte(`{"model":"m","metadata":{"user_id":"user_abc_session_1"},"messages":[{"role":"user","content":"hi"}]}`)
	fromMeta := conversationKeyForRequest(http.Header{}, body, 7)
	if fromMeta == "" {
		t.Fatal("metadata.user_id 存在时应得到会话标识")
	}
	other := []byte(`{"model":"m","metadata":{"user_id":"user_abc_session_2"},"messages":[{"role":"user","content":"hi"}]}`)
	if conversationKeyForRequest(http.Header{}, other, 7) == fromMeta {
		t.Fatal("不同 metadata.user_id 必须是不同会话")
	}
}

func TestConversationKeyFallsBackToFirstUserMessage(t *testing.T) {
	first := []byte(`{"model":"m","messages":[{"role":"system","content":"sys"},{"role":"user","content":"帮我重构这段代码"}]}`)
	key := conversationKeyForRequest(http.Header{}, first, 7)
	if key == "" {
		t.Fatal("无会话头时应退回首条用户消息指纹")
	}
	// 多轮对话：首条用户消息不变，后续追加 assistant/tool 消息仍属同一段对话。
	second := []byte(`{"model":"m","messages":[{"role":"system","content":"sys"},{"role":"user","content":"帮我重构这段代码"},{"role":"assistant","content":"好的"},{"role":"user","content":"继续"}]}`)
	if conversationKeyForRequest(http.Header{}, second, 7) != key {
		t.Fatal("同一段对话的不同轮次必须命中同一会话标识")
	}
	// 不同对话（首条消息不同）必须分开。
	third := []byte(`{"model":"m","messages":[{"role":"user","content":"写一个快排"}]}`)
	if conversationKeyForRequest(http.Header{}, third, 7) == key {
		t.Fatal("首条消息不同必须视为不同对话")
	}
}

func TestConversationKeySupportsResponsesAndAnthropicShapes(t *testing.T) {
	responses := []byte(`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"部署一下"}]}]}`)
	anthropic := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"部署一下"}]}]}`)
	key := conversationKeyForRequest(http.Header{}, responses, 7)
	if key == "" {
		t.Fatal("Responses input 形状必须能识别首条用户消息")
	}
	if conversationKeyForRequest(http.Header{}, anthropic, 7) == "" {
		t.Fatal("Anthropic parts 形状必须能识别首条用户消息")
	}
}

func TestConversationKeyEmptyWhenUndetectable(t *testing.T) {
	if key := conversationKeyForRequest(http.Header{}, []byte(`{"model":"m"}`), 7); key != "" {
		t.Fatalf("既无会话头也无用户消息时应返回空串，实际 %q", key)
	}
	// 只有 assistant 消息（续写场景）同样识别不出会话，不做粘性。
	onlyAssistant := []byte(`{"model":"m","messages":[{"role":"assistant","content":"好的"}]}`)
	if key := conversationKeyForRequest(http.Header{}, onlyAssistant, 7); key != "" {
		t.Fatalf("没有用户消息时不应做粘性，实际 %q", key)
	}
}

func TestApplyConversationChannelAffinityKeepsPinnedFirst(t *testing.T) {
	candidates := []Candidate{
		{ChannelID: 5, ChannelModelID: 50},
		{ChannelID: 3, ChannelModelID: 30},
		{ChannelID: 9, ChannelModelID: 90},
	}
	ordered := applyConversationChannelAffinity(candidates, []uint64{9, 5, 3})
	want := []uint64{9, 5, 3}
	for index, channelID := range want {
		if ordered[index].ChannelID != channelID {
			t.Fatalf("第 %d 位应为渠道 %d，实际 %d", index, channelID, ordered[index].ChannelID)
		}
	}
}

func TestApplyConversationChannelAffinitySkipsUnavailableAndAppendsNew(t *testing.T) {
	// 记忆里渠道 9 当前不可用（不在候选里，代表被禁用/关闭时段过滤），渠道 10 是本轮新增的。
	candidates := []Candidate{
		{ChannelID: 10, ChannelModelID: 100},
		{ChannelID: 3, ChannelModelID: 30},
		{ChannelID: 5, ChannelModelID: 50},
	}
	ordered := applyConversationChannelAffinity(candidates, []uint64{9, 5, 3})
	want := []uint64{5, 3, 10}
	if len(ordered) != len(candidates) {
		t.Fatalf("重排不得增删候选，实际 %d 个", len(ordered))
	}
	for index, channelID := range want {
		if ordered[index].ChannelID != channelID {
			t.Fatalf("第 %d 位应为渠道 %d，实际 %d", index, channelID, ordered[index].ChannelID)
		}
	}
}

func TestApplyConversationChannelAffinityKeepsSameChannelModels(t *testing.T) {
	// 同一渠道可能有多个模型映射行，重排必须把它们全部保留且不丢候选。
	candidates := []Candidate{
		{ChannelID: 3, ChannelModelID: 31},
		{ChannelID: 3, ChannelModelID: 32},
		{ChannelID: 5, ChannelModelID: 51},
	}
	ordered := applyConversationChannelAffinity(candidates, []uint64{5, 3})
	if len(ordered) != 3 {
		t.Fatalf("重排后候选数应保持 3，实际 %d", len(ordered))
	}
	if ordered[0].ChannelID != 5 {
		t.Fatalf("记忆优先的渠道 5 应排第一，实际 %d", ordered[0].ChannelID)
	}
	if ordered[1].ChannelID != 3 || ordered[2].ChannelID != 3 {
		t.Fatal("同渠道的多个模型行必须全部保留")
	}
}

func TestCandidateChannelIDsDeduplicates(t *testing.T) {
	ids := candidateChannelIDs([]Candidate{
		{ChannelID: 3, ChannelModelID: 31},
		{ChannelID: 3, ChannelModelID: 32},
		{ChannelID: 5, ChannelModelID: 51},
	})
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 5 {
		t.Fatalf("渠道顺序去重结果错误：%v", ids)
	}
}
