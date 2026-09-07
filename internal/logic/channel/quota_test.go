package channel

import (
	"strings"
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

func TestParseQuotaResponseCreditLimit(t *testing.T) {
	// 积分制新套餐：窗口类型为 CREDIT_LIMIT，带完整数值（5cec7a63 实测响应）。
	body := []byte(`{"code":200,"msg":"操作成功","data":{"limits":[` +
		`{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":12000,"currentValue":265,"remaining":11734,"percentage":2,"nextResetTime":1788663578110},` +
		`{"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":60000,"currentValue":10,"remaining":59990,"percentage":1,"nextResetTime":1789200000000}` +
		`],"level":"pro"},"success":true}`)
	view, err := parseQuotaResponse("zhipu_coding_plan", body)
	if err != nil {
		t.Fatalf("parseQuotaResponse error: %v", err)
	}
	if view.Level != "pro" {
		t.Fatalf("Level = %q, want pro", view.Level)
	}
	if len(view.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(view.Windows))
	}
	fiveHour, weekly := view.Windows[0], view.Windows[1]
	if fiveHour.Kind != QuotaWindowFiveHour || weekly.Kind != QuotaWindowWeekly {
		t.Fatalf("kinds = %s/%s, want five_hour/weekly", fiveHour.Kind, weekly.Kind)
	}
	if fiveHour.Used == nil || *fiveHour.Used != 265 || fiveHour.Total == nil || *fiveHour.Total != 12000 || fiveHour.Remaining == nil || *fiveHour.Remaining != 11734 {
		t.Fatalf("five hour values = %v/%v/%v, want 265/12000/11734", fiveHour.Used, fiveHour.Total, fiveHour.Remaining)
	}
	if fiveHour.UsedPercent != 2 || weekly.UsedPercent != 1 {
		t.Fatalf("percents = %v/%v, want 2/1", fiveHour.UsedPercent, weekly.UsedPercent)
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

func TestParseAFPResponse(t *testing.T) {
	// 依官方文档结构构造：Agent Plan 档位仅 5 小时/周/月三窗口有效，
	// 近一天 AFPDaily 为占位（Quota=0），应被跳过。数值为字符串形式。
	body := []byte(`{"ResponseMetadata":{"RequestId":"x","Action":"GetAFPUsage","Version":"2024-01-01","Service":"ark","Region":"cn-beijing"},` +
		`"Result":{"PlanType":"universe",` +
		`"AFPFiveHour":{"Quota":"100.0","Used":"38.0","SubscribeTime":0,"ResetTime":1788700000000},` +
		`"AFPDaily":{"Quota":"0","Used":"0","SubscribeTime":0,"ResetTime":0},` +
		`"AFPWeekly":{"Quota":"300.0","Used":"60.0","SubscribeTime":0,"ResetTime":1789200000000},` +
		`"AFPMonthly":{"Quota":"1200.0","Used":"240.0","SubscribeTime":0,"ResetTime":1790000000000}}}`)
	view, err := parseAFPResponse(body)
	if err != nil {
		t.Fatalf("parseAFPResponse error: %v", err)
	}
	if view.Mode != "volcengine_afp" || view.Level != "universe" {
		t.Fatalf("mode/level = %q/%q, want volcengine_afp/universe", view.Mode, view.Level)
	}
	if len(view.Windows) != 3 {
		t.Fatalf("windows = %d, want 3", len(view.Windows))
	}
	fiveHour, weekly, monthly := view.Windows[0], view.Windows[1], view.Windows[2]
	if fiveHour.Kind != QuotaWindowFiveHour || weekly.Kind != QuotaWindowWeekly || monthly.Kind != QuotaWindowMonthly {
		t.Fatalf("kinds = %s/%s/%s, want five_hour/weekly/monthly", fiveHour.Kind, weekly.Kind, monthly.Kind)
	}
	if fiveHour.Used == nil || *fiveHour.Used != 38 || fiveHour.Total == nil || *fiveHour.Total != 100 || fiveHour.Remaining == nil || *fiveHour.Remaining != 62 {
		t.Fatalf("five hour values = %v/%v/%v, want 38/100/62", fiveHour.Used, fiveHour.Total, fiveHour.Remaining)
	}
	if fiveHour.UsedPercent != 38 || weekly.UsedPercent != 20 || monthly.UsedPercent != 20 {
		t.Fatalf("percents = %v/%v/%v, want 38/20/20", fiveHour.UsedPercent, weekly.UsedPercent, monthly.UsedPercent)
	}
	if fiveHour.NextResetAt == nil || fiveHour.NextResetAt.UnixMilli() != 1788700000000 {
		t.Fatalf("five hour reset = %v, want epoch 1788700000000", fiveHour.NextResetAt)
	}
}

func TestParseAFPResponseError(t *testing.T) {
	body := []byte(`{"ResponseMetadata":{"Error":{"Code":"AccessDenied","Message":"The requested action is not permitted"}},"Result":{}}`)
	_, err := parseAFPResponse(body)
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err = %v, want AccessDenied message", err)
	}
}
