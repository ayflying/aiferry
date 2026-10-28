package channel

import (
	"context"
	"testing"
)

func affinityRows(ids ...uint64) []credentialRow {
	rows := make([]credentialRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, credentialRow{Id: id, ApiKeyCipher: "cipher"})
	}
	return rows
}

func TestSelectConversationCredentialPicksNewOnFirstTurn(t *testing.T) {
	rows := affinityRows(1, 2, 3)
	for attempt := 0; attempt < 20; attempt++ {
		selected, persist := selectConversationCredential(rows, 0, nil, map[uint64]comboHealth{})
		if !persist {
			t.Fatal("新对话首次挑选必须落记忆")
		}
		found := false
		for _, row := range rows {
			if row.Id == selected.Id {
				found = true
			}
		}
		if !found {
			t.Fatalf("选中了不存在的密钥 %d", selected.Id)
		}
	}
}

func TestSelectConversationCredentialReusesAffinity(t *testing.T) {
	rows := affinityRows(1, 2, 3)
	// 记忆是 2，且 2 仍可用：无论跑多少次都必须是 2，且不重写记忆。
	for attempt := 0; attempt < 30; attempt++ {
		selected, persist := selectConversationCredential(rows, 2, nil, map[uint64]comboHealth{})
		if selected.Id != 2 {
			t.Fatalf("应固定复用记忆中的密钥 2，实际 %d", selected.Id)
		}
		if persist {
			t.Fatal("记忆仍可用时不应改写记忆")
		}
	}
}

func TestSelectConversationCredentialTempExcludeKeepsAffinity(t *testing.T) {
	rows := affinityRows(1, 2, 3)
	// 记忆是 2，但本次请求里 2 已经失败过：本次换一把，但不覆盖记忆。
	selected, persist := selectConversationCredential(rows, 2, map[uint64]struct{}{2: {}}, map[uint64]comboHealth{})
	if selected.Id == 2 {
		t.Fatal("被本次请求排除的密钥不应再被选中")
	}
	if persist {
		t.Fatal("请求内的临时失败不得覆盖会话记忆，否则一次抖动就把整段对话永久换走")
	}
}

func TestSelectConversationCredentialReplacesWhenAffinityUnavailable(t *testing.T) {
	// 记忆是 2，但 2 已不在可用池里（禁用/冷却/欠费被摘掉）：改记新的一把。
	rows := affinityRows(1, 3)
	selected, persist := selectConversationCredential(rows, 2, nil, map[uint64]comboHealth{})
	if !persist {
		t.Fatal("原密钥不可用时必须改写记忆")
	}
	if selected.Id != 1 && selected.Id != 3 {
		t.Fatalf("应改用剩余可用密钥，实际 %d", selected.Id)
	}
}

func TestSelectConversationCredentialNoUsableRow(t *testing.T) {
	rows := affinityRows(1, 2)
	selected, persist := selectConversationCredential(rows, 1, map[uint64]struct{}{1: {}, 2: {}}, map[uint64]comboHealth{})
	if selected.Id != 0 || persist {
		t.Fatalf("全部被排除时应返回空结果，实际 id=%d persist=%v", selected.Id, persist)
	}
}

func TestConversationAffinityContextRoundTrip(t *testing.T) {
	base := context.Background()
	if got := conversationAffinityFromContext(base); got != "" {
		t.Fatalf("空 ctx 不应有会话标识，实际 %q", got)
	}
	ctx := WithConversationAffinity(base, "abc")
	if got := conversationAffinityFromContext(ctx); got != "abc" {
		t.Fatalf("会话标识应可读回，实际 %q", got)
	}
	// 空白标识不写入：没有会话的请求保持原有按键级绑定行为。
	if got := conversationAffinityFromContext(WithConversationAffinity(base, "   ")); got != "" {
		t.Fatalf("空白会话标识应被忽略，实际 %q", got)
	}
}

func TestCredentialAffinityKeyIncludesModel(t *testing.T) {
	a := credentialAffinityKey("conv", 35, 100)
	b := credentialAffinityKey("conv", 35, 200)
	if a == b {
		t.Fatal("不同模型必须使用不同的密钥记忆，否则切换模型会互相踩")
	}
	if credentialAffinityKey("conv", 35, 100) != a {
		t.Fatal("同一会话、渠道、模型必须得到同一个键")
	}
}
