package channel

import (
	"encoding/json"
	"testing"

	"github.com/yunloli/aiferry/internal/model/entity"
)

func testRule(model, name string) syncedRule {
	return syncedRule{Model: model, Name: name, Currency: "USD", Conditions: json.RawMessage(`{}`), Rates: json.RawMessage(`{"inputPerMillion":1}`)}
}

// 别名渠道（UpstreamName=gpt-5.6-sol → PublicName=gpt-6-sol）的上游价，
// 不得混入已有公开名直接来源的 gpt-6-sol：同步后应保持 2 条而不是 4 条。
func TestBuildPublicRulePlanSkipsAliasWhenPublicSourceExists(t *testing.T) {
	byPublic := map[string][]entity.ChannelModels{
		"gpt-6-sol": {
			{Id: 55, PublicName: "gpt-6-sol", UpstreamName: "gpt-5.6-sol"},
			{Id: 215, PublicName: "gpt-6-sol", UpstreamName: "gpt-6-sol"},
		},
	}
	byUpstream := map[string][]entity.ChannelModels{
		"gpt-5.6-sol": {{Id: 55, PublicName: "gpt-6-sol", UpstreamName: "gpt-5.6-sol"}},
		"gpt-6-sol":   {{Id: 215, PublicName: "gpt-6-sol", UpstreamName: "gpt-6-sol"}},
	}
	rules := []syncedRule{
		testRule("gpt-6-sol", "短上下文"),
		testRule("gpt-6-sol", "长上下文"),
		testRule("gpt-5.6-sol", "别名短上下文"),
		testRule("gpt-5.6-sol", "别名长上下文"),
	}
	publicRules, canonical := buildPublicRulePlan(byPublic, byUpstream, rules)
	got := publicRules["gpt-6-sol"]
	if len(got) != 2 {
		t.Fatalf("alias must not merge into covered public model: got %d rules, want 2: %+v", len(got), got)
	}
	for _, rule := range got {
		if rule.Model != "gpt-6-sol" {
			t.Fatalf("unexpected alias rule merged: %+v", rule)
		}
	}
	if canonical["gpt-6-sol"] != 55 {
		t.Fatalf("canonical id got %d, want 55", canonical["gpt-6-sol"])
	}
}

// 公开名没有任何直接来源时，上游名别名匹配正常补位。
func TestBuildPublicRulePlanAliasFillsGap(t *testing.T) {
	byPublic := map[string][]entity.ChannelModels{
		"gpt-6-sol": {{Id: 55, PublicName: "gpt-6-sol", UpstreamName: "gpt-5.6-sol"}},
	}
	byUpstream := map[string][]entity.ChannelModels{
		"gpt-5.6-sol": {{Id: 55, PublicName: "gpt-6-sol", UpstreamName: "gpt-5.6-sol"}},
	}
	rules := []syncedRule{
		testRule("gpt-5.6-sol", "别名短上下文"),
		testRule("gpt-5.6-sol", "别名长上下文"),
	}
	publicRules, canonical := buildPublicRulePlan(byPublic, byUpstream, rules)
	if got := len(publicRules["gpt-6-sol"]); got != 2 {
		t.Fatalf("alias should fill gap: got %d rules, want 2", got)
	}
	if canonical["gpt-6-sol"] != 55 {
		t.Fatalf("canonical id got %d, want 55", canonical["gpt-6-sol"])
	}
}

// 上游名带厂商前缀、本地只有别名映射时，后缀回退仍能补位。
func TestBuildPublicRulePlanAliasSuffixFallback(t *testing.T) {
	byPublic := map[string][]entity.ChannelModels{
		"glm-5.3-flash": {{Id: 9, PublicName: "glm-5.3-flash", UpstreamName: "custom-glm"}},
	}
	byUpstream := map[string][]entity.ChannelModels{
		"custom-glm": {{Id: 9, PublicName: "glm-5.3-flash", UpstreamName: "custom-glm"}},
	}
	rules := []syncedRule{testRule("z-ai/glm-5.3-flash", "后缀价")}
	publicRules, _ := buildPublicRulePlan(byPublic, byUpstream, rules)
	if got := len(publicRules["glm-5.3-flash"]); got != 1 {
		t.Fatalf("suffix fallback via public name: got %d, want 1", got)
	}

	publicRules, _ = buildPublicRulePlan(map[string][]entity.ChannelModels{}, byUpstream, []syncedRule{
		testRule("vendor/custom-glm", "上游别名后缀价"),
	})
	if got := len(publicRules["glm-5.3-flash"]); got != 1 {
		t.Fatalf("suffix fallback via upstream alias: got %d, want 1", got)
	}
}

// 上游价格条目带厂商前缀（z-ai/glm-5.3-flash）而本地模型没有前缀时，
// 应回退用最后一个 / 之后的后缀匹配 glm-5.3-flash。
func TestNameLookupFallsBackToSuffixAfterSlash(t *testing.T) {
	lookup := newNameLookup(map[string][]entity.ChannelModels{
		"glm-5.3-flash": {{Id: 1, PublicName: "glm-5.3-flash", UpstreamName: "glm-5.3-flash"}},
	})
	matches := lookup.match("z-ai/glm-5.3-flash")
	if len(matches) != 1 || matches[0].Model.PublicName != "glm-5.3-flash" || matches[0].Rank != ruleMatchSuffix {
		t.Fatalf("suffix fallback failed: got %+v", matches)
	}
}

// 精确匹配命中时不得走后缀回退，避免歧义。
func TestNameLookupPrefersExactMatch(t *testing.T) {
	lookup := newNameLookup(map[string][]entity.ChannelModels{
		"z-ai/glm-5.3-flash": {{Id: 2, PublicName: "z-ai/glm-5.3-flash", UpstreamName: "z-ai/glm-5.3-flash"}},
		"glm-5.3-flash":      {{Id: 1, PublicName: "glm-5.3-flash", UpstreamName: "glm-5.3-flash"}},
	})
	matches := lookup.match("z-ai/glm-5.3-flash")
	if len(matches) != 1 || matches[0].Model.Id != 2 || matches[0].Rank != ruleMatchExact {
		t.Fatalf("exact match should win: got %+v", matches)
	}
}

// 本地模型本身带前缀且与上游同名时正常精确匹配。
func TestNameLookupExactWithPrefix(t *testing.T) {
	lookup := newNameLookup(map[string][]entity.ChannelModels{
		"z-ai/glm-5.3-flash": {{Id: 3, PublicName: "z-ai/glm-5.3-flash"}},
	})
	matches := lookup.match("z-ai/glm-5.3-flash")
	if len(matches) != 1 || matches[0].Model.Id != 3 {
		t.Fatalf("prefixed exact match failed: got %+v", matches)
	}
}

// 后缀无法命中时返回空，且无前缀名称不受影响。
func TestNameLookupNoMatchAndPlainName(t *testing.T) {
	lookup := newNameLookup(map[string][]entity.ChannelModels{
		"glm-5.3-flash": {{Id: 1, PublicName: "glm-5.3-flash"}},
	})
	if matches := lookup.match("other-vendor/glm-4-air"); len(matches) != 0 {
		t.Fatalf("unrelated suffix should not match: got %+v", matches)
	}
	matches := lookup.match("glm-5.3-flash")
	if len(matches) != 1 || matches[0].Model.Id != 1 {
		t.Fatalf("plain name match failed: got %+v", matches)
	}
}

// 上游模型名大小写与本地不一致（OpenAI/GPT-4O vs gpt-4o）是常见差异，必须命中。
func TestNameLookupFoldsCase(t *testing.T) {
	lookup := newNameLookup(map[string][]entity.ChannelModels{
		"gpt-4o": {{Id: 7, PublicName: "gpt-4o", UpstreamName: "gpt-4o"}},
	})
	matches := lookup.match("OpenAI/GPT-4O")
	if len(matches) != 1 || matches[0].Model.Id != 7 {
		t.Fatalf("case-insensitive match failed: got %+v", matches)
	}
}

// 微调/量化变体（:peft:、:sampling-）在上游有独立条目且价格更高，
// 只有本地没有标准名条目时才允许用变体价兜底；否则必须让标准价胜出。
func TestBuildPublicRulePlanPrefersStandardOverVariant(t *testing.T) {
	byPublic := map[string][]entity.ChannelModels{
		"glm-5.3": {{Id: 11, PublicName: "glm-5.3", UpstreamName: "glm-5.3"}},
	}
	rules := []syncedRule{
		testRule("glm-5.3", "标准价"),
		testRule("zai-org/GLM-5.3:peft:262144", "微调变体价"),
	}
	publicRules, _ := buildPublicRulePlan(byPublic, map[string][]entity.ChannelModels{}, rules)
	got := publicRules["glm-5.3"]
	if len(got) != 1 || got[0].Name != "标准价" {
		t.Fatalf("variant price must not override the standard price: %+v", got)
	}
}

func TestBuildPublicRulePlanFallsBackToVariant(t *testing.T) {
	byPublic := map[string][]entity.ChannelModels{
		"glm-5.3": {{Id: 11, PublicName: "glm-5.3", UpstreamName: "glm-5.3"}},
	}
	publicRules, _ := buildPublicRulePlan(byPublic, map[string][]entity.ChannelModels{}, []syncedRule{
		testRule("zai-org/GLM-5.3:peft:262144", "微调变体价"),
	})
	got := publicRules["glm-5.3"]
	if len(got) != 1 || got[0].Name != "微调变体价" {
		t.Fatalf("variant price should fill a gap: %+v", got)
	}
}

func TestNormalizeModelNameStripsVendorAndVariantSuffixes(t *testing.T) {
	cases := map[string]string{
		"zai-org/GLM-5.3:peft:262144":                        "glm-5.3",
		"thinkingmachines/Inkling:peft:262144:sampling-nvfp4": "inkling",
		"Qwen/Qwen3-8B":                                      "qwen3-8b",
		"glm-5.3":                                            "glm-5.3",
		"  GPT-4O  ":                                         "gpt-4o",
	}
	for input, want := range cases {
		if got := normalizeModelName(input); got != want {
			t.Fatalf("normalizeModelName(%q) = %q, want %q", input, got, want)
		}
	}
}
