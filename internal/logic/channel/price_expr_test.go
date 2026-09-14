package channel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func decodeJSONObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return decoded
}

func TestParseBillingExpressionFlatPricing(t *testing.T) {
	breakdowns, err := parseBillingExpression(`tier("standard", p * 3 + cr * 0.3 + cc * 3.75 + cc1h * 6 + c * 15)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(breakdowns) != 1 {
		t.Fatalf("expected a single branch, got %#v", breakdowns)
	}
	branch := breakdowns[0]
	if branch.Tier != "standard" || branch.Timezone != "" || branch.MinInput != nil || branch.MaxInput != nil {
		t.Fatalf("expected an unconditional standard branch: %#v", branch)
	}
	want := map[string]float64{"inputPerMillion": 3, "cachedInputPerMillion": 0.3, "cacheWritePerMillion": 3.75, "outputPerMillion": 15}
	if len(branch.Rates) != len(want) {
		t.Fatalf("unexpected rates: %#v", branch.Rates)
	}
	for key, value := range want {
		if branch.Rates[key] != value {
			t.Fatalf("rate %s = %v, want %v", key, branch.Rates[key], value)
		}
	}
}

func TestParseBillingExpressionUsesOneHourCachePriceAsFallback(t *testing.T) {
	breakdowns, err := parseBillingExpression(`tier("standard", p * 1 + cc1h * 2 + c * 4)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := breakdowns[0].Rates["cacheWritePerMillion"]; got != 2 {
		t.Fatalf("expected 1h cache write price as fallback, got %v", got)
	}
}

func TestParseBillingExpressionMapsAudioAndFixedRates(t *testing.T) {
	breakdowns, err := parseBillingExpression(`tier("standard", p * 1 + ai * 2 + ao * 6 + fixed(0.04))`)
	if err != nil {
		t.Fatal(err)
	}
	rates := breakdowns[0].Rates
	if rates["audioInputPerMillion"] != 2 || rates["audioOutputPerMillion"] != 6 || rates["request"] != 0.04 {
		t.Fatalf("unexpected rates: %#v", rates)
	}
}

func TestParseBillingExpressionContextTiers(t *testing.T) {
	breakdowns, err := parseBillingExpression(`len <= 512000 ? tier("0_512k", p * 0.3 + cr * 0.06 + c * 1.2) : tier("512k_plus", p * 0.6 + cr * 0.12 + c * 2.4)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(breakdowns) != 2 {
		t.Fatalf("expected two context tiers, got %#v", breakdowns)
	}
	short, long := breakdowns[0], breakdowns[1]
	if short.MaxInput == nil || *short.MaxInput != 512000 || short.MinInput != nil {
		t.Fatalf("unexpected short-context bounds: %#v", short)
	}
	if long.MinInput == nil || *long.MinInput != 512001 || long.MaxInput != nil {
		t.Fatalf("unexpected long-context bounds: %#v", long)
	}
	if short.Rates["inputPerMillion"] != 0.3 || long.Rates["inputPerMillion"] != 0.6 {
		t.Fatalf("unexpected tier rates: %#v %#v", short.Rates, long.Rates)
	}
}

func TestParseBillingExpressionChainedContextTiers(t *testing.T) {
	breakdowns, err := parseBillingExpression(`len <= 32000 ? tier("0_32k", p * 0.45 + c * 2.25) : len <= 128000 ? tier("32k_128k", p * 0.75 + c * 3.75) : tier("128k_plus", p * 1.2 + c * 6)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(breakdowns) != 3 {
		t.Fatalf("expected three context tiers, got %#v", breakdowns)
	}
	middle := breakdowns[1]
	if middle.MinInput == nil || *middle.MinInput != 32001 || middle.MaxInput == nil || *middle.MaxInput != 128000 {
		t.Fatalf("unexpected middle tier bounds: %#v", middle)
	}
	if middle.Rates["outputPerMillion"] != 3.75 {
		t.Fatalf("unexpected middle tier rates: %#v", middle.Rates)
	}
	last := breakdowns[2]
	if last.MinInput == nil || *last.MinInput != 128001 || last.MaxInput != nil {
		t.Fatalf("unexpected last tier bounds: %#v", last)
	}
}

func TestParseBillingScheduleConditionParsesPeakWindow(t *testing.T) {
	schedule, err := parseBillingScheduleCondition(`weekday("UTC") >= 1 && weekday("UTC") <= 5 && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10))`)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.Timezone != "UTC" {
		t.Fatalf("unexpected timezone: %q", schedule.Timezone)
	}
	if len(schedule.Weekdays) != 5 || schedule.Weekdays[0] != 1 || schedule.Weekdays[4] != 5 {
		t.Fatalf("unexpected weekdays: %#v", schedule.Weekdays)
	}
	if len(schedule.HourRanges) != 2 || schedule.HourRanges[0] != [2]string{"01:00", "04:00"} || schedule.HourRanges[1] != [2]string{"06:00", "10:00"} {
		t.Fatalf("unexpected hour ranges: %#v", schedule.HourRanges)
	}
}

// 峰谷表达式展开成两条规则：高峰档带 conditions.time，非高峰档不带条件作兜底。
// 兜底规则必须排在前面（先入库 → id 更小 → 计费时后试），否则高峰价会被
// 兜底价盖掉；反过来若高峰档不带时段条件，则高价会覆盖全天。
func TestParseBillingExpressionScheduleSplitsPeakAndFallback(t *testing.T) {
	expression := `weekday("UTC") >= 1 && weekday("UTC") <= 5 && ((hour("UTC") >= 1 && hour("UTC") < 4) || (hour("UTC") >= 6 && hour("UTC") < 10)) ? tier("peak", p * 1.32 + cr * 0.044 + c * 3.96) : tier("off_peak", p * 0.66 + cr * 0.022 + c * 1.98)`
	breakdowns, err := parseBillingExpression(expression)
	if err != nil {
		t.Fatal(err)
	}
	if len(breakdowns) != 2 {
		t.Fatalf("expected a fallback branch and a peak branch, got %#v", breakdowns)
	}
	fallback, peak := breakdowns[0], breakdowns[1]
	if fallback.Timezone != "" || len(fallback.Weekdays) != 0 || len(fallback.HourRanges) != 0 {
		t.Fatalf("fallback branch must stay unconditional: %#v", fallback)
	}
	if fallback.Rates["inputPerMillion"] != 0.66 || fallback.Rates["cachedInputPerMillion"] != 0.022 || fallback.Rates["outputPerMillion"] != 1.98 {
		t.Fatalf("unexpected off-peak rates: %#v", fallback.Rates)
	}
	if !strings.Contains(fallback.Note, "非高峰档") {
		t.Fatalf("expected an off-peak note, got %q", fallback.Note)
	}
	if peak.Timezone != "UTC" {
		t.Fatalf("unexpected peak timezone: %q", peak.Timezone)
	}
	if len(peak.Weekdays) != 5 || peak.Weekdays[0] != 1 || peak.Weekdays[4] != 5 {
		t.Fatalf("unexpected peak weekdays: %#v", peak.Weekdays)
	}
	if len(peak.HourRanges) != 2 || peak.HourRanges[0] != [2]string{"01:00", "04:00"} || peak.HourRanges[1] != [2]string{"06:00", "10:00"} {
		t.Fatalf("unexpected peak hour ranges: %#v", peak.HourRanges)
	}
	if peak.Rates["inputPerMillion"] != 1.32 || peak.Rates["outputPerMillion"] != 3.96 {
		t.Fatalf("unexpected peak rates: %#v", peak.Rates)
	}
	if !strings.Contains(peak.Note, "高峰") {
		t.Fatalf("expected a peak note, got %q", peak.Note)
	}

	fallbackRule := billingExprSyncedRule("deepseek-flash", fallback)
	peakRule := billingExprSyncedRule("deepseek-flash", peak)
	if string(fallbackRule.Conditions) != "{}" {
		t.Fatalf("fallback rule must be unconditional: %s", fallbackRule.Conditions)
	}
	timeBlock := decodeJSONObject(t, peakRule.Conditions)["time"]
	block, ok := timeBlock.(map[string]any)
	if !ok {
		t.Fatalf("peak rule must carry a time block: %s", peakRule.Conditions)
	}
	if block["tz"] != "UTC" {
		t.Fatalf("unexpected time block timezone: %#v", block["tz"])
	}
	if ranges, ok := block["ranges"].([]any); !ok || len(ranges) != 2 {
		t.Fatalf("unexpected time block ranges: %#v", block["ranges"])
	}
	// 入库顺序：兜底先、高峰后，保证计费按 id 降序时先试高峰档。
	ordered := orderSyncedRules([]syncedRule{peakRule, fallbackRule})
	if ordered[0].Name != fallbackRule.Name || ordered[1].Name != peakRule.Name {
		t.Fatalf("fallback rule must be inserted first: %#v", ordered)
	}
}

func TestParseBillingExpressionThinkingToggleKeepsDefaultBranch(t *testing.T) {
	breakdowns, err := parseBillingExpression(`param("enable_thinking") == true ? tier("thinking", p * 0.4 + c * 4) : tier("standard", p * 0.4 + c * 1.2)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(breakdowns) != 1 {
		t.Fatalf("expected a single usable branch, got %#v", breakdowns)
	}
	branch := breakdowns[0]
	if branch.Tier != "standard" || branch.Rates["outputPerMillion"] != 1.2 {
		t.Fatalf("expected the non-thinking branch: %#v", branch)
	}
	if !strings.Contains(branch.Note, "enable_thinking") {
		t.Fatalf("expected a downgrade note, got %q", branch.Note)
	}
}

func TestParseBillingExpressionRejectsUnsupportedShapes(t *testing.T) {
	for _, expression := range []string{
		`tier("image", fixed(0.04)) * image_count`,
		`tier("standard", p * 1 * len)`,
		`tier("standard", tokens * 3)`,
		`param("x") == true ? tier("a", p * 1) : tier("b", p * 2) ? tier("c", p * 3)`,
		`unknown_flag ? tier("a", p * 1) : tier("b", p * 2)`,
	} {
		if _, err := parseBillingExpression(expression); err == nil {
			t.Fatalf("expected %q to fail", expression)
		}
	}
}

func TestSyncedRulesFromBillingExpressionsSkipsUnknownModes(t *testing.T) {
	rules, err := syncedRulesFromBillingExpressions(
		gjson.Parse(`{
			"gpt-5": "tier(\"standard\", p * 3 + c * 15)",
			"flat-image": "tier(\"standard\", fixed(1))"
		}`),
		gjson.Parse(`{"gpt-5": "tiered_expr", "flat-image": "fixed"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Model != "gpt-5" {
		t.Fatalf("unexpected rules: %#v", rules)
	}
	if rules[0].Name != billingExprRuleName {
		t.Fatalf("unconditional rules must keep the default name: %q", rules[0].Name)
	}
	if string(rules[0].Conditions) != "{}" {
		t.Fatalf("unexpected conditions: %s", rules[0].Conditions)
	}
}

func TestSyncedRulesFromNewAPIRatioSupportsBillingExpressions(t *testing.T) {
	rules, err := syncedRulesFromNewAPIRatio([]byte(`{
  "data": {
    "billing_mode": {"gpt-5": "tiered_expr", "gemini-2.5-pro": "tiered_expr"},
    "billing_expr": {
      "gpt-5": "tier(\"standard\", p * 1.25 + cr * 0.125 + c * 10)",
      "gemini-2.5-pro": "len <= 200000 ? tier(\"0_200k\", p * 1.25 + c * 10) : tier(\"200k_plus\", p * 2.5 + c * 15)"
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected three rules, got %#v", rules)
	}
	if rules[0].Model != "gpt-5" || rules[1].Model != "gemini-2.5-pro" || rules[2].Model != "gemini-2.5-pro" {
		t.Fatalf("unexpected rule order: %#v", rules)
	}
	tierConditions := decodeJSONObject(t, rules[1].Conditions)
	if value, ok := tierConditions["inputTokensAtMost"]; !ok || value != float64(200000) {
		t.Fatalf("unexpected short-context conditions: %#v", tierConditions)
	}
	longConditions := decodeJSONObject(t, rules[2].Conditions)
	if value, ok := longConditions["inputTokensAtLeast"]; !ok || value != float64(200001) {
		t.Fatalf("unexpected long-context conditions: %#v", longConditions)
	}
	rates := decodeJSONObject(t, rules[0].Rates)
	if rates["inputPerMillion"] != 1.25 || rates["cachedInputPerMillion"] != 0.125 || rates["outputPerMillion"] != float64(10) {
		t.Fatalf("unexpected flat rates: %#v", rates)
	}
}

func TestSyncedRulesFromBillingExpressionsNeedsUsableModel(t *testing.T) {
	if _, err := syncedRulesFromBillingExpressions(
		gjson.Parse(`{"weird":"tier(\"standard\", p * 1 * len)"}`),
		gjson.Parse(`{"weird":"tiered_expr"}`),
	); err == nil {
		t.Fatal("expected an unparsable source to fail")
	}
	if _, err := syncedRulesFromNewAPIRatio([]byte(`{"data":{"billing_mode":{"gpt-5":"tiered_expr"}}}`)); err == nil {
		t.Fatal("expected a payload without priced fields to fail")
	}
}

func TestOrderSyncedRulesPutsFallbackFirst(t *testing.T) {
	rules := []syncedRule{
		{Model: "m", Name: "peak", Conditions: json.RawMessage(`{"time":{"tz":"UTC"}}`)},
		{Model: "m", Name: "fallback", Conditions: json.RawMessage(`{}`)},
	}
	ordered := orderSyncedRules(rules)
	if ordered[0].Name != "fallback" || ordered[1].Name != "peak" {
		t.Fatalf("fallback rules must be inserted first: %#v", ordered)
	}
	if rules[0].Name != "peak" {
		t.Fatal("orderSyncedRules must not mutate its input")
	}
}

func TestRuleHasConditionsIgnoresEmptyObjects(t *testing.T) {
	for _, raw := range []string{`{}`, ``, `null`} {
		if ruleHasConditions(syncedRule{Conditions: json.RawMessage(raw)}) {
			t.Fatalf("%q must be treated as unconditional", raw)
		}
	}
	if !ruleHasConditions(syncedRule{Conditions: json.RawMessage(`{"inputTokensAtMost":1}`)}) {
		t.Fatal("token range conditions must be detected")
	}
}
