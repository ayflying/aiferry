package channel

import (
	"context"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 本文件负责「同步规则 → 落到哪个公开模型 → 写库」这一段：名称匹配与择优、
// 零价防护、清理历史同步规则。上游载荷解析见 price_payload.go，
// 计费表达式见 price_expr*.go，名称归一化见 price_name.go。

// ruleBucket 按公开名累积规则，并按匹配强度择优：同一个公开模型只保留强度
// 最高的一组条目。上游同时给出标准名与 peft 变体名时（glm-5.3 与
// zai-org/GLM-5.3:peft:262144），标准名的精确匹配强度更高，变体高价因此
// 不会覆盖标准价；两者都只有弱匹配时才用变体价兜底。
type ruleBucket struct {
	ranks     map[string]int
	rules     map[string][]syncedRule
	canonical map[string]uint64
	excluded  map[string]struct{}
}

func newRuleBucket() *ruleBucket {
	return &ruleBucket{
		ranks:     make(map[string]int),
		rules:     make(map[string][]syncedRule),
		canonical: make(map[string]uint64),
		excluded:  make(map[string]struct{}),
	}
}

func (b *ruleBucket) add(model entity.ChannelModels, rule syncedRule, rank int) {
	if _, skip := b.excluded[model.PublicName]; skip {
		return
	}
	switch current, exists := b.ranks[model.PublicName]; {
	case !exists || rank > current:
		b.ranks[model.PublicName] = rank
		b.rules[model.PublicName] = []syncedRule{rule}
	case rank == current:
		b.rules[model.PublicName] = append(b.rules[model.PublicName], rule)
	}
	if b.canonical[model.PublicName] == 0 || model.Id < b.canonical[model.PublicName] {
		b.canonical[model.PublicName] = model.Id
	}
}

// matchPublicModels 把一条上游条目的命中结果按公开名去重：同一个公开名可能
// 对应多个渠道模型（多个渠道各有一条 channel_models），它们共用一个公开价格，
// 不去重会让同一条上游价写入多份重复规则。
func matchPublicModels(lookup *nameLookup, ruleModel string) []ruleModelMatch {
	matches := lookup.match(ruleModel)
	if len(matches) < 2 {
		return matches
	}
	seen := make(map[string]struct{}, len(matches))
	unique := matches[:0]
	for _, match := range matches {
		if _, exists := seen[match.Model.PublicName]; exists {
			continue
		}
		seen[match.Model.PublicName] = struct{}{}
		unique = append(unique, match)
	}
	return unique
}

// buildPublicRulePlan 把上游价格条目两级匹配到各公开模型，返回
// 公开名 → 规则列表 与 公开名 → 规范 channel_model id。
//
// 第一级按公开名直接匹配（含大小写、厂商前缀、变体后缀回退），命中的公开名
// 记为已覆盖；第二级按上游名（别名）匹配，仅补公开名没有直接来源的缺口。
// 这样当上游 key 自身已是某个公开名时，别名渠道（UpstreamName 指向别的
// 上游模型）的另一套价格不会混进该公开模型——典型事故是
// channel_models 把 gpt-5.6-sol 别名到公开名 gpt-6-sol，同步时
// gpt-5.6-sol 的 2 倍价规则与 gpt-6-sol 自身 2 条合并成 4 条。
func buildPublicRulePlan(byPublic, byUpstream map[string][]entity.ChannelModels, rules []syncedRule) (map[string][]syncedRule, map[string]uint64) {
	direct := newRuleBucket()
	publicLookup := newNameLookup(byPublic)
	for _, rule := range rules {
		for _, match := range matchPublicModels(publicLookup, rule.Model) {
			direct.add(match.Model, rule, match.Rank)
		}
	}

	// 第二级只处理第一级完全没覆盖的公开名，且强度择优在缺口内部单独进行。
	alias := newRuleBucket()
	for name := range direct.rules {
		alias.excluded[name] = struct{}{}
	}
	upstreamLookup := newNameLookup(byUpstream)
	for _, rule := range rules {
		for _, match := range matchPublicModels(upstreamLookup, rule.Model) {
			alias.add(match.Model, rule, match.Rank)
		}
	}

	publicRules := make(map[string][]syncedRule, len(direct.rules)+len(alias.rules))
	canonicalModelIDs := make(map[string]uint64, len(direct.rules)+len(alias.rules))
	for name, modelRules := range direct.rules {
		publicRules[name] = modelRules
	}
	for name, modelRules := range alias.rules {
		publicRules[name] = modelRules
	}
	for name, modelID := range direct.canonical {
		canonicalModelIDs[name] = modelID
	}
	for name, modelID := range alias.canonical {
		canonicalModelIDs[name] = modelID
	}
	return publicRules, canonicalModelIDs
}

// priceSyncStats 是一次价格同步的成果统计。
type priceSyncStats struct {
	// Count 是写入的价格规则条数。
	Count int
	// Skipped 是上游给出但不可用的条目（系数全为 0、表达式无法解析等）。
	Skipped []PriceSyncSkip
}

func (s *sChannel) syncPricesFromPayload(ctx context.Context, endpoint string, config channeltype.PricingConfig, body []byte) (priceSyncStats, error) {
	rules, skips, err := syncedRulesFromJSON(body, config)
	if err != nil {
		return priceSyncStats{}, err
	}
	if len(rules) == 0 {
		return priceSyncStats{Skipped: skips}, gerror.New("upstream price query did not return price rules")
	}
	return s.saveSyncedPriceRules(ctx, endpoint, rules, skips)
}

func (s *sChannel) saveSyncedPriceRules(ctx context.Context, endpoint string, rules []syncedRule, skips []PriceSyncSkip) (priceSyncStats, error) {
	var models []entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Scan(&models); err != nil {
		return priceSyncStats{}, gerror.Wrap(err, "load public models for prices")
	}
	byPublic := make(map[string][]entity.ChannelModels, len(models))
	byUpstream := make(map[string][]entity.ChannelModels, len(models))
	for _, model := range models {
		byPublic[model.PublicName] = append(byPublic[model.PublicName], model)
		byUpstream[model.UpstreamName] = append(byUpstream[model.UpstreamName], model)
	}

	// 上游「按量价格为 0」的条目在解析阶段已经拦掉，这里再兜一层：其他价格源
	// （普通 JSON 列表）同样可能给出全 0 费率。0 价规则最危险——模型会被判成
	// 已定价却永远按 0 计费，所以宁可当作「没有价格」处理。
	usable, zeroSkips := splitZeroPricedRules(rules)
	skips = append(skips, zeroSkips...)

	publicRules, canonicalModelIDs := buildPublicRulePlan(byPublic, byUpstream, usable)
	staleNames := staleSyncedPriceModels(byPublic, byUpstream, publicRules, skips)

	count := 0
	err := dao.ModelPriceRules.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		for modelName, modelRules := range publicRules {
			if _, deleteErr := dao.ModelPriceRules.Ctx(txCtx).Where(do.ModelPriceRules{ModelName: modelName, Source: "sync"}).Delete(); deleteErr != nil {
				return gerror.Wrap(deleteErr, "replace synced price rules")
			}
			conditional := false
			for _, rule := range orderSyncedRules(modelRules) {
				if ruleHasConditions(rule) {
					conditional = true
				}
				if _, insertErr := dao.ModelPriceRules.Ctx(txCtx).Data(do.ModelPriceRules{ChannelModelId: canonicalModelIDs[modelName], ModelName: modelName, Name: rule.Name, Source: "sync", SourceRef: endpoint, Currency: rule.Currency, ConditionsJson: string(rule.Conditions), RatesJson: string(rule.Rates), Status: 1, SyncedAt: gtime.Now()}).Insert(); insertErr != nil {
					return gerror.Wrap(insertErr, "save synced price rule")
				}
				count++
				if values, ok := modelPriceValuesFromRule(rule); ok {
					if saveErr := s.mergePublicPrice(txCtx, modelName, values); saveErr != nil {
						return saveErr
					}
				}
			}
			// 分档模型（峰谷/上下文阶梯）只有走规则计费才能命中对应档位，
			// 因此这里把它的公共价格切到 rules 模式；无条件模型不受影响。
			if conditional {
				if modeErr := s.setPublicBillingMode(txCtx, modelName, BillingModeRules); modeErr != nil {
					return modeErr
				}
			}
		}
		// 上游本次明确「不计费」的模型要清掉历史同步规则，并抹掉历史 0 价：
		// 旧版本会把「系数全为 0」的表达式落成 0 价规则与 0 价公共价格，于是
		// 模型被判成已定价却永远按 0 计费，还会因「已定价」被要求余额预检。
		// 只清理同步规则而不清公共价格，这次修复对这些模型等于没有生效。
		for _, modelName := range staleNames {
			if _, deleteErr := dao.ModelPriceRules.Ctx(txCtx).Where(do.ModelPriceRules{ModelName: modelName, Source: "sync"}).Delete(); deleteErr != nil {
				return gerror.Wrap(deleteErr, "clear skipped synced price rules")
			}
			if clearErr := s.clearZeroPublicPrice(txCtx, modelName); clearErr != nil {
				return clearErr
			}
		}
		return nil
	})
	if err != nil {
		return priceSyncStats{}, err
	}
	if err = s.prices.Load(ctx); err != nil {
		return priceSyncStats{}, err
	}
	return priceSyncStats{Count: count, Skipped: skips}, nil
}

// splitZeroPricedRules 把全 0 费率的规则从同步列表里剔除，并转成跳过记录。
func splitZeroPricedRules(rules []syncedRule) ([]syncedRule, []PriceSyncSkip) {
	if len(rules) == 0 {
		return rules, nil
	}
	usable := make([]syncedRule, 0, len(rules))
	var skips []PriceSyncSkip
	for _, rule := range rules {
		if rulePricesNothing(rule) {
			skips = append(skips, PriceSyncSkip{Model: rule.Model, Reason: "上游按量价格为 0（多为订阅制或免费额度），跳过以免按 0 计费"})
			continue
		}
		usable = append(usable, rule)
	}
	return usable, skips
}

// rulePricesNothing 判断规则费率是否全为 0 或全部缺失：这种规则没有任何计费
// 意义，落库只会把模型伪装成「已定价」。
func rulePricesNothing(rule syncedRule) bool {
	rates := gjson.ParseBytes(rule.Rates)
	if !rates.IsObject() {
		return true
	}
	priced := false
	for _, field := range priceRateFields {
		value := rates.Get(field)
		if value.Exists() && value.Type == gjson.Number && value.Float() != 0 {
			priced = true
			break
		}
	}
	return !priced
}

// priceRateFields 是费率 JSON 里所有计费维度，顺序与费率结构一致。
var priceRateFields = []string{
	"inputPerMillion", "cachedInputPerMillion", "cacheWritePerMillion", "outputPerMillion",
	"imageInputPerMillion", "audioInputPerMillion", "audioOutputPerMillion", "request",
}

// staleSyncedPriceModels 找出「本次没有可用价格、但可能残留历史同步规则」的公开名：
// 跳过条目按公开名 / 上游名两级解析到的模型，只要没被本次成功规则覆盖就要清理。
func staleSyncedPriceModels(byPublic, byUpstream map[string][]entity.ChannelModels, planned map[string][]syncedRule, skips []PriceSyncSkip) []string {
	if len(skips) == 0 {
		return nil
	}
	publicLookup := newNameLookup(byPublic)
	upstreamLookup := newNameLookup(byUpstream)
	stale := make(map[string]struct{})
	collect := func(matches []ruleModelMatch) {
		for _, match := range matches {
			if _, covered := planned[match.Model.PublicName]; !covered {
				stale[match.Model.PublicName] = struct{}{}
			}
		}
	}
	for _, skip := range skips {
		collect(publicLookup.match(skip.Model))
		collect(upstreamLookup.match(skip.Model))
	}
	names := make([]string, 0, len(stale))
	for name := range stale {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// clearZeroPublicPrice 把全为 0 的公共价格字段清成 NULL。
// 0 与 NULL 在计费上完全相反：NULL 表示「没有价格」（不计费也不做余额预检），
// 0 表示「已定价且价格是 0」（永远免费，且因 IsPriced 为真被要求余额预检）。
// 只在规则已清空之后调用，避免误删仍有规则在用的模型。
func (s *sChannel) clearZeroPublicPrice(ctx context.Context, modelName string) error {
	var current PublicModelView
	if err := dao.ModelPrices.Ctx(ctx).
		Where(dao.ModelPrices.Columns().PublicName, modelName).
		Scan(&current); err != nil {
		if isMissingPublicPriceError(err) {
			return nil
		}
		return gerror.Wrap(err, "load public price for zero price cleanup")
	}
	if current.PublicName == "" {
		return nil
	}
	data := do.ModelPrices{}
	changed := false
	clearZero := func(value *float64, target *any) {
		if value != nil && *value == 0 {
			*target = gdb.Raw("NULL")
			changed = true
		}
	}
	clearZero(current.InputPrice, &data.InputPrice)
	clearZero(current.CachedInputPrice, &data.CachedInputPrice)
	clearZero(current.CacheWritePrice, &data.CacheWritePrice)
	clearZero(current.OutputPrice, &data.OutputPrice)
	clearZero(current.ImageInputPrice, &data.ImageInputPrice)
	clearZero(current.AudioInputPrice, &data.AudioInputPrice)
	clearZero(current.AudioOutputPrice, &data.AudioOutputPrice)
	clearZero(current.RequestPrice, &data.RequestPrice)
	if !changed {
		return nil
	}
	if _, err := dao.ModelPrices.Ctx(ctx).Where(dao.ModelPrices.Columns().PublicName, modelName).Data(data).Update(); err != nil {
		return gerror.Wrap(err, "clear zero public price")
	}
	return nil
}

// orderSyncedRules 让无条件规则先入库、条件规则后入库。规则匹配按 id 降序
// 逐条试错，后入库的条件规则（峰谷价、上下文阶梯）因此先于兜底规则命中。
func orderSyncedRules(rules []syncedRule) []syncedRule {
	ordered := make([]syncedRule, len(rules))
	copy(ordered, rules)
	sort.SliceStable(ordered, func(i, j int) bool {
		return !ruleHasConditions(ordered[i]) && ruleHasConditions(ordered[j])
	})
	return ordered
}

func ruleHasConditions(rule syncedRule) bool {
	conditions := strings.TrimSpace(string(rule.Conditions))
	return conditions != "" && conditions != "{}" && conditions != "null"
}
