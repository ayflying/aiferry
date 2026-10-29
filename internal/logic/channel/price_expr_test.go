package channel

import (
	"encoding/json"
	"errors"
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

// cc12h 是上游 2026-10 新增的 12 小时缓存写入价变量，OpenAI 新一代模型
// （gpt-5.6 / gpt-6 / gpt-daybreak）的表达式全都引用它。不认识它的后果是
// 这些模型整条被丢弃、完全没有价格。
func TestParseBillingExpressionMapsTwelveHourCacheWrite(t *testing.T) {
	breakdowns, err := parseBillingExpression(`tier("standard", p * 4 + cr * 0.4 + cc * 5 + cc12h * 10 + c * 20)`)
	if err != nil {
		t.Fatal(err)
	}
	rates := breakdowns[0].Rates
	if rates["cacheWritePerMillion"] != 5 {
		t.Fatalf("short TTL cache write price must win over 12h: %#v", rates)
	}
	if rates["inputPerMillion"] != 4 || rates["outputPerMillion"] != 20 {
		t.Fatalf("unexpected rates: %#v", rates)
	}

	// cc 缺席时按 TTL 从短到长兜底：cc1h 优先于 cc12h，避免高 TTL 价覆盖低 TTL 档。
	breakdowns, err = parseBillingExpression(`tier("standard", p * 1 + cc1h * 2 + cc12h * 4 + c * 2)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := breakdowns[0].Rates["cacheWritePerMillion"]; got != 2 {
		t.Fatalf("expected 1h cache write price to win over 12h, got %v", got)
	}
	breakdowns, err = parseBillingExpression(`tier("standard", p * 1 + cc12h * 4 + c * 2)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := breakdowns[0].Rates["cacheWritePerMillion"]; got != 4 {
		t.Fatalf("expected 12h cache write price as the last fallback, got %v", got)
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

// fixed(x) * image_count 是「每张图 x 美元」。图像生成端点不返回 usage，
// 折算成图像 tokens 会让规则永远算不出金额（请求直接报「用量不可计费」），
// 因此按次计价落库，与上游 model_price 的既有口径一致。
func TestParseBillingExpressionMapsPerImagePriceToRequestPrice(t *testing.T) {
	breakdowns, err := parseBillingExpression(`tier("image", fixed(0.0035)) * image_count`)
	if err != nil {
		t.Fatal(err)
	}
	rates := breakdowns[0].Rates
	if rates["request"] != 0.0035 {
		t.Fatalf("per-image price must become a per-request rate: %#v", rates)
	}
	if _, hasImageRate := rates["imageInputPerMillion"]; hasImageRate {
		t.Fatalf("image tokens must not be used, the endpoint reports no usage: %#v", rates)
	}
}

// 系数为 0 的项不构成价格：上游用 p * 0 表示该维度不计费，不能落成 0 价费率。
func TestParseBillingExpressionDropsZeroCoefficients(t *testing.T) {
	breakdowns, err := parseBillingExpression(`tier("standard", p * 3 + cr * 0 + cc * 0 + c * 15)`)
	if err != nil {
		t.Fatal(err)
	}
	rates := breakdowns[0].Rates
	if _, exists := rates["cachedInputPerMillion"]; exists {
		t.Fatalf("zero cached input rate must be dropped: %#v", rates)
	}
	if _, exists := rates["cacheWritePerMillion"]; exists {
		t.Fatalf("zero cache write rate must be dropped: %#v", rates)
	}
	if rates["inputPerMillion"] != 3 || rates["outputPerMillion"] != 15 {
		t.Fatalf("unexpected rates: %#v", rates)
	}
}

// 系数全为 0 的表达式表示订阅制或免费额度，必须判为「没有价格」。
// 若当成 0 价规则落库，模型会被判成已定价却永远按 0 计费，等于对所有人免费。
func TestParseBillingExpressionRejectsZeroPricedExpression(t *testing.T) {
	for _, expression := range []string{
		`tier("standard", p * 0 + cr * 0 + cc * 0 + c * 0)`,
		`tier("standard", p * 0 + c * 0)`,
		`tier("image", fixed(0)) * image_count`,
	} {
		if _, err := parseBillingExpression(expression); !errors.Is(err, errBillingExprNoPrice) {
			t.Fatalf("%q must be reported as unpriced, got %v", expression, err)
		}
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
	rules, skips, err := syncedRulesFromBillingExpressions(
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
	if len(skips) != 1 || skips[0].Model != "flat-image" || skips[0].Reason == "" {
		t.Fatalf("unexpected skips: %#v", skips)
	}
}

// 订阅制模型（系数全为 0）要作为跳过记录返回并说明原因，而不是静默消失。
func TestSyncedRulesFromBillingExpressionsReportsZeroPricedModels(t *testing.T) {
	rules, skips, err := syncedRulesFromBillingExpressions(
		gjson.Parse(`{
			"gpt-5": "tier(\"standard\", p * 3 + c * 15)",
			"glm-4.7-flash": "tier(\"standard\", p * 0 + cr * 0 + cc * 0 + c * 0)"
		}`),
		gjson.Parse(`{"gpt-5": "tiered_expr", "glm-4.7-flash": "tiered_expr"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Model != "gpt-5" {
		t.Fatalf("zero priced models must not become rules: %#v", rules)
	}
	if len(skips) != 1 || skips[0].Model != "glm-4.7-flash" {
		t.Fatalf("unexpected skips: %#v", skips)
	}
	if !strings.Contains(skips[0].Reason, "未按量计费") {
		t.Fatalf("skip reason must explain the zero price: %q", skips[0].Reason)
	}
}

func TestSyncedRulesFromNewAPIRatioSupportsBillingExpressions(t *testing.T) {
	rules, _, err := syncedRulesFromNewAPIRatio([]byte(`{
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
	if rules[0].Model != "gemini-2.5-pro" || rules[1].Model != "gemini-2.5-pro" || rules[2].Model != "gpt-5" {
		t.Fatalf("unexpected rule order: %#v", rules)
	}
	tierConditions := decodeJSONObject(t, rules[0].Conditions)
	if value, ok := tierConditions["inputTokensAtMost"]; !ok || value != float64(200000) {
		t.Fatalf("unexpected short-context conditions: %#v", tierConditions)
	}
	longConditions := decodeJSONObject(t, rules[1].Conditions)
	if value, ok := longConditions["inputTokensAtLeast"]; !ok || value != float64(200001) {
		t.Fatalf("unexpected long-context conditions: %#v", longConditions)
	}
	rates := decodeJSONObject(t, rules[2].Rates)
	if rates["inputPerMillion"] != 1.25 || rates["cachedInputPerMillion"] != 0.125 || rates["outputPerMillion"] != float64(10) {
		t.Fatalf("unexpected flat rates: %#v", rates)
	}
}

func TestSyncedRulesFromBillingExpressionsNeedsUsableModel(t *testing.T) {
	if _, _, err := syncedRulesFromBillingExpressions(
		gjson.Parse(`{"weird":"tier(\"standard\", p * 1 * len)"}`),
		gjson.Parse(`{"weird":"tiered_expr"}`),
	); err == nil {
		t.Fatal("expected an unparsable source to fail")
	}
	if _, _, err := syncedRulesFromNewAPIRatio([]byte(`{"data":{"billing_mode":{"gpt-5":"tiered_expr"}}}`)); err == nil {
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

// 全 0 费率规则在任何价格源里都不能落库，否则模型会被判成已定价却永远按 0 计费。
func TestSplitZeroPricedRulesDropsUnpricedRows(t *testing.T) {
	rules := []syncedRule{
		{Model: "free", Rates: json.RawMessage(`{"inputPerMillion":0,"outputPerMillion":0}`)},
		{Model: "empty", Rates: json.RawMessage(`{}`)},
		{Model: "paid", Rates: json.RawMessage(`{"inputPerMillion":0,"outputPerMillion":2.5}`)},
	}
	usable, skips := splitZeroPricedRules(rules)
	if len(usable) != 1 || usable[0].Model != "paid" {
		t.Fatalf("only priced rules may be saved: %#v", usable)
	}
	if len(skips) != 2 || skips[0].Model != "free" || skips[1].Model != "empty" {
		t.Fatalf("unexpected skips: %#v", skips)
	}
}
