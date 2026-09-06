package channel

import (
	"testing"
	"time"
)

func floatPtr(value float64) *float64 { return &value }

func timePtr(value time.Time) *time.Time { return &value }

func TestMergeQuotaViewsSingleCredential(t *testing.T) {
	views := []QuotaView{{
		Mode:  "zhipu_coding_plan",
		Level: "GLM-4.5",
		Windows: []QuotaWindow{
			{Kind: QuotaWindowFiveHour, Label: "5 小时额度", UsedPercent: 42.5},
			{Kind: QuotaWindowWeekly, Label: "每周额度", UsedPercent: 18},
		},
	}}
	merged := mergeQuotaViews(views)
	if merged.Level != "GLM-4.5" {
		t.Fatalf("Level = %q, want GLM-4.5", merged.Level)
	}
	if len(merged.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(merged.Windows))
	}
	if merged.Windows[0].UsedPercent != 42.5 || merged.Windows[1].UsedPercent != 18 {
		t.Fatalf("percent = %v/%v, want 42.5/18", merged.Windows[0].UsedPercent, merged.Windows[1].UsedPercent)
	}
}

func TestMergeQuotaViewsMultipleCredentials(t *testing.T) {
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.Local)
	views := []QuotaView{
		{
			Level: "GLM-4.5",
			Windows: []QuotaWindow{
				{Kind: QuotaWindowFiveHour, Label: "5 小时额度", UsedPercent: 40, NextResetAt: timePtr(base)},
				{Kind: QuotaWindowMCP, Label: "MCP 月度调用", UsedPercent: 20, Used: floatPtr(10), Total: floatPtr(100), Remaining: floatPtr(90)},
			},
		},
		{
			Level: "GLM-4.6",
			Windows: []QuotaWindow{
				{Kind: QuotaWindowFiveHour, Label: "5 小时额度", UsedPercent: 60, NextResetAt: timePtr(base.Add(2 * time.Hour))},
				{Kind: QuotaWindowWeekly, Label: "每周额度", UsedPercent: 30},
				{Kind: QuotaWindowMCP, Label: "MCP 月度调用", UsedPercent: 50, Used: floatPtr(25), Total: floatPtr(100), Remaining: floatPtr(75)},
			},
		},
	}
	merged := mergeQuotaViews(views)
	if merged.Level != "GLM-4.5 / GLM-4.6" {
		t.Fatalf("Level = %q, want merged levels", merged.Level)
	}
	byKind := make(map[string]QuotaWindow, len(merged.Windows))
	for _, window := range merged.Windows {
		byKind[window.Kind] = window
	}
	// 百分比窗口取平均：(40+60)/2 = 50；重置时间取最早的 base。
	fiveHour := byKind[QuotaWindowFiveHour]
	if fiveHour.UsedPercent != 50 {
		t.Fatalf("five hour percent = %v, want 50", fiveHour.UsedPercent)
	}
	if fiveHour.NextResetAt == nil || !fiveHour.NextResetAt.Equal(base) {
		t.Fatalf("five hour reset = %v, want %v", fiveHour.NextResetAt, base)
	}
	// 拥有该窗口的密钥才计入分母：只有第二个密钥有每周窗口，30 保持不变。
	if byKind[QuotaWindowWeekly].UsedPercent != 30 {
		t.Fatalf("weekly percent = %v, want 30", byKind[QuotaWindowWeekly].UsedPercent)
	}
	// MCP 调用次数累加：10+25=35 已用、100+100=200 总量、90+75=165 剩余、百分比平均 35。
	mcp := byKind[QuotaWindowMCP]
	if mcp.Used == nil || *mcp.Used != 35 || mcp.Total == nil || *mcp.Total != 200 || mcp.Remaining == nil || *mcp.Remaining != 165 {
		t.Fatalf("mcp used/total/remaining = %v/%v/%v, want 35/200/165", mcp.Used, mcp.Total, mcp.Remaining)
	}
	if mcp.UsedPercent != 35 {
		t.Fatalf("mcp percent = %v, want 35", mcp.UsedPercent)
	}
}

func TestMergeQuotaViewsPartialFailureMessage(t *testing.T) {
	ciphers := []quotaCredential{{Prefix: "ab****ef"}, {Prefix: "cd****gh"}}
	failures := []string{"HTTP 401: unauthorized", ""}
	success := []QuotaView{{Level: "GLM-4.5", Windows: []QuotaWindow{{Kind: QuotaWindowFiveHour, Label: "5 小时额度", UsedPercent: 25}}}}
	_ = success
	// 模拟 fetchQuota 的失败聚合：失败明细应带上密钥前缀。
	messages := make([]string, 0, len(ciphers))
	for index := range ciphers {
		if failures[index] != "" {
			messages = append(messages, "密钥 "+ciphers[index].Prefix+"："+failures[index])
		}
	}
	if len(messages) != 1 || messages[0] != "密钥 ab****ef：HTTP 401: unauthorized" {
		t.Fatalf("messages = %v, want one prefixed failure", messages)
	}
}
