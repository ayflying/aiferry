package channel

import (
	"testing"

	"github.com/yunloli/aiferry/internal/model/entity"
)

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
