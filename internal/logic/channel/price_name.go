package channel

import (
	"strings"

	"github.com/yunloli/aiferry/internal/model/entity"
)

// 匹配强度：数值越大越可信。同一个公开模型可能被多条上游条目命中，此时只保留
// 强度最高的一组，避免弱匹配的价格覆盖强匹配的价格。
//
// 上游 llm-metadata 的 key 与本地公开名有四种常见差异，只做精确匹配会漏掉
// 相当一部分条目：
//
//	厂商前缀  z-ai/glm-5.3-flash        Qwen/Qwen3-8B
//	大小写    GLM-5.3                   MiniMax-M2
//	变体后缀  zai-org/GLM-5.3:peft:262144                       （微调/量化变体）
//	          thinkingmachines/Inkling:peft:262144:sampling-nvfp4 （变体叠加采样）
//
// 变体条目价常是标准名的数倍（glm-5.3 标准 1.4 倍、peft 变体 3.78 倍），
// 所以变体匹配固定在最低强度：只要上游同时给出标准名，标准名的强匹配就赢。
const (
	ruleMatchNone    = 0
	ruleMatchVariant = 1 // 去掉厂商前缀与 peft / sampling 后缀后同名
	ruleMatchSuffix  = 2 // 最后一个 / 之后的名称同名
	ruleMatchFold    = 3 // 整体忽略大小写同名
	ruleMatchExact   = 4 // 与本地名称逐字符相同
)

// nameLookup 是「名称 → 本地模型」索引及其两种宽松视图，构建一次供全部上游
// 条目复用：逐条遍历索引会让 382 条上游规则 × 数千个本地模型变成平方级。
type nameLookup struct {
	byName       map[string][]entity.ChannelModels
	byFold       map[string][]string // 全小写名称 → 原始键
	byNormalized map[string][]string // 归一化短名 → 原始键
}

// ruleModelMatch 是一条上游模型名命中的本地模型及其匹配强度。
type ruleModelMatch struct {
	Model entity.ChannelModels
	Rank  int
}

func newNameLookup(byName map[string][]entity.ChannelModels) *nameLookup {
	lookup := &nameLookup{
		byName:       byName,
		byFold:       make(map[string][]string, len(byName)),
		byNormalized: make(map[string][]string, len(byName)),
	}
	for key := range byName {
		folded := strings.ToLower(key)
		lookup.byFold[folded] = append(lookup.byFold[folded], key)
		if normalized := normalizeModelName(key); normalized != "" {
			lookup.byNormalized[normalized] = append(lookup.byNormalized[normalized], key)
		}
	}
	return lookup
}

// match 按强度从高到低逐级尝试，命中某级即返回该级结果（不再往下兜底），
// 因此同一条上游条目不会同时以两种强度命中同一个公开模型。
func (l *nameLookup) match(ruleModel string) []ruleModelMatch {
	ruleModel = strings.TrimSpace(ruleModel)
	if ruleModel == "" {
		return nil
	}
	if models, exists := l.byName[ruleModel]; exists {
		return ruleModelMatches(models, ruleMatchExact)
	}
	if matches := l.matchFolded(ruleModel, ruleMatchFold); len(matches) > 0 {
		return matches
	}
	if slash := strings.LastIndex(ruleModel, "/"); slash >= 0 {
		if suffix := strings.TrimSpace(ruleModel[slash+1:]); suffix != "" {
			if models, exists := l.byName[suffix]; exists {
				return ruleModelMatches(models, ruleMatchSuffix)
			}
			if matches := l.matchFolded(suffix, ruleMatchSuffix); len(matches) > 0 {
				return matches
			}
		}
	}
	if !isVariantModelName(ruleModel) {
		return nil
	}
	// 变体后缀只对带 :peft: / :sampling- 的名称兜底。反过来（本地名带变体、
	// 上游是标准名）不需要处理：标准名会在前面几级直接命中。
	return l.matchNormalized(normalizeModelName(ruleModel))
}

func (l *nameLookup) matchFolded(name string, rank int) []ruleModelMatch {
	matches := make([]ruleModelMatch, 0, 1)
	for _, key := range l.byFold[strings.ToLower(name)] {
		matches = append(matches, ruleModelMatches(l.byName[key], rank)...)
	}
	return matches
}

func (l *nameLookup) matchNormalized(normalized string) []ruleModelMatch {
	if normalized == "" {
		return nil
	}
	matches := make([]ruleModelMatch, 0, 1)
	for _, key := range l.byNormalized[normalized] {
		matches = append(matches, ruleModelMatches(l.byName[key], ruleMatchVariant)...)
	}
	return matches
}

func ruleModelMatches(models []entity.ChannelModels, rank int) []ruleModelMatch {
	matches := make([]ruleModelMatch, 0, len(models))
	for _, model := range models {
		matches = append(matches, ruleModelMatch{Model: model, Rank: rank})
	}
	return matches
}

// isVariantModelName 判断上游名称是否带 peft 微调或 sampling 量化变体后缀。
func isVariantModelName(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, ":peft:") || strings.Contains(name, ":sampling-")
}

// normalizeModelName 去掉大小写、厂商前缀与变体后缀，得到用于兜底比较的短名。
// 变体后缀可能叠加（:peft:262144:sampling-nvfp4），循环剥到不再变化为止。
func normalizeModelName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for {
		stripped := name
		if index := strings.Index(stripped, ":peft:"); index >= 0 {
			stripped = stripped[:index]
		}
		if index := strings.Index(stripped, ":sampling-"); index >= 0 {
			stripped = stripped[:index]
		}
		if stripped == name {
			break
		}
		name = stripped
	}
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	return strings.TrimSpace(name)
}
