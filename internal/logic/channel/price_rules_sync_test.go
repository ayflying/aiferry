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
func TestMatchModelsForRuleFallsBackToSuffixAfterSlash(t *testing.T) {
	byName := map[string][]entity.ChannelModels{
		"glm-5.3-flash": {{Id: 1, PublicName: "glm-5.3-flash", UpstreamName: "glm-5.3-flash"}},
	}
	models := matchModelsForRule(byName, "z-ai/glm-5.3-flash")
	if len(models) != 1 || models[0].PublicName != "glm-5.3-flash" {
		t.Fatalf("suffix fallback failed: got %+v, want glm-5.3-flash", models)
	}
}

// 精确匹配命中时不得走后缀回退，避免歧义。
func TestMatchModelsForRulePrefersExactMatch(t *testing.T) {
	byName := map[string][]entity.ChannelModels{
		"z-ai/glm-5.3-flash": {{Id: 2, PublicName: "z-ai/glm-5.3-flash", UpstreamName: "z-ai/glm-5.3-flash"}},
		"glm-5.3-flash":      {{Id: 1, PublicName: "glm-5.3-flash", UpstreamName: "glm-5.3-flash"}},
	}
	models := matchModelsForRule(byName, "z-ai/glm-5.3-flash")
	if len(models) != 1 || models[0].Id != 2 {
		t.Fatalf("exact match should win: got %+v, want z-ai/glm-5.3-flash", models)
	}
}

// 本地模型本身带前缀且与上游同名时正常精确匹配。
func TestMatchModelsForRuleExactWithPrefix(t *testing.T) {
	byName := map[string][]entity.ChannelModels{
		"z-ai/glm-5.3-flash": {{Id: 3, PublicName: "z-ai/glm-5.3-flash"}},
	}
	models := matchModelsForRule(byName, "z-ai/glm-5.3-flash")
	if len(models) != 1 || models[0].Id != 3 {
		t.Fatalf("prefixed exact match failed: got %+v", models)
	}
}

// 后缀无法命中时返回空，且无前缀名称不受影响。
func TestMatchModelsForRuleNoMatchAndPlainName(t *testing.T) {
	byName := map[string][]entity.ChannelModels{
		"glm-5.3-flash": {{Id: 1, PublicName: "glm-5.3-flash"}},
	}
	if models := matchModelsForRule(byName, "other-vendor/glm-4-air"); len(models) != 0 {
		t.Fatalf("unrelated suffix should not match: got %+v", models)
	}
	models := matchModelsForRule(byName, "glm-5.3-flash")
	if len(models) != 1 || models[0].Id != 1 {
		t.Fatalf("plain name match failed: got %+v", models)
	}
}
