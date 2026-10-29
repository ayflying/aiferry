package channel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
)

// BaseLLM（llm-metadata）自 2026-09 起把倍率配置换成 new-api 计费表达式：
// data.billing_expr 是 model -> expr 的映射，data.billing_mode 固定为
// tiered_expr，同时删除了 model_ratio / completion_ratio / cache_ratio /
// model_price 这些旧字段。表达式是 expr-lang 语法、系数为 USD/1M tokens
// 实价，结构固定为「时段 → 思考模式 → 上下文长度」三层三元，叶子统一是
// tier("<档名>", <线性表达式>)，变量含义：
//
//	p  输入 tokens 价      c  输出 tokens 价
//	cr 缓存读取价          cc 缓存写入价（5 分钟档）
//	cc1h 1 小时缓存写入价  cc12h 12 小时缓存写入价
//	ai 音频输入价          ao 音频输出价
//	fixed(x) 每次请求固定价，或在末尾 * image_count 时表示每张图 x 美元
//	image_count 图片张数（AiFerry 无该维度，按次计价）
//
// 这里把它翻译成 AiFerry 的价格规则：时段映射到 conditions.time（上游用
// UTC），上下文长度映射到 inputTokensAtMost / inputTokensAtLeast。思考模式
// 依赖请求体参数（param("enable_thinking")），网关没有对应的规则条件，
// 只能取 else 分支（未开启思考）计价，并在规则名里标注。
//
// 三个容易踩的坑：
//   - AiFerry 只有一个缓存写入费率字段，cc1h / cc12h 按 TTL 从短到长兜底，
//     不能被高 TTL 价覆盖低 TTL 档。
//   - 系数全为 0 的表达式表示订阅制 / 免费额度，必须判为无价格跳过：写成
//     0 价规则会让模型显得「已定价」却永远按 0 计费（见 errBillingExprNoPrice）。
//   - 按张计费的 fixed(x) * image_count 不能丢：丢掉会让图像模型变成免费；
//     但它必须落成按次费率而非图像 tokens，因为 /images/* 不返回 usage。
//
// 代码按职责拆在同包三个文件里：本文件负责「上游载荷 → 同步规则」的入口与
// 表达式递归展开，price_expr_rates.go 负责费率项解析，price_expr_condition.go
// 负责条件（时段 / 长度 / 参数）解析。

const (
	billingExprTieredMode = "tiered_expr"
	billingExprRuleName   = "BaseLLM 官方模型价格"
)

// billingExprBreakdown 是表达式递归展开后的一条价格分支：
// 时段与长度条件都为空表示这条规则适用于全部请求。
type billingExprBreakdown struct {
	Tier       string
	Rates      map[string]float64
	Timezone   string
	Weekdays   []int
	HourRanges [][2]string
	MinInput   *uint64
	MaxInput   *uint64
	Note       string
}

// syncedRulesFromBillingExpressions 把 billing_expr 映射转换成同步规则。
// 单个模型解析失败只跳过该模型（不阻断整个价格源），全部失败时返回错误，
// 以便管理员看到上游格式再次变化的信号。
//
// 被跳过的条目连同中文原因一起返回：上游有几十个订阅制/免费额度模型的价格
// 表达式系数全为 0，静默跳过会让管理员误以为同步正常，实际那些模型没有价格。
func syncedRulesFromBillingExpressions(expressions, modes gjson.Result) ([]syncedRule, []PriceSyncSkip, error) {
	if !expressions.IsObject() {
		return nil, nil, gerror.New("NewAPI billing expression source did not return an object")
	}
	rules := make([]syncedRule, 0, len(expressions.Map())+8)
	skips := make([]PriceSyncSkip, 0)
	expressions.ForEach(func(model, expression gjson.Result) bool {
		name := strings.TrimSpace(model.String())
		if name == "" {
			return true
		}
		if mode := strings.TrimSpace(modes.Get(name).String()); mode != "" && mode != billingExprTieredMode {
			skips = append(skips, PriceSyncSkip{Model: name, Reason: fmt.Sprintf("计费模式不是 %s（%s），暂不支持", billingExprTieredMode, mode)})
			return true
		}
		breakdowns, err := parseBillingExpression(expression.String())
		if err != nil {
			skips = append(skips, PriceSyncSkip{Model: name, Reason: billingExprSkipReason(err)})
			return true
		}
		for _, breakdown := range breakdowns {
			rules = append(rules, billingExprSyncedRule(name, breakdown))
		}
		return true
	})
	if len(rules) == 0 {
		return nil, skips, gerror.Newf("NewAPI billing expression source had no usable priced model (%d skipped)", len(skips))
	}
	// 只按模型名稳定排序，保持同一模型内各档位的原始顺序：入库顺序决定计费
	// 时的规则优先级（见 orderSyncedRules），同档位被打乱会让阶梯价错位。
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Model < rules[j].Model })
	return rules, skips, nil
}

// billingExprSkipReason 把解析错误翻译成管理端可读的跳过原因。
func billingExprSkipReason(err error) string {
	if isBillingExprNoPriceError(err) {
		return "上游未按量计费（系数全为 0，多为订阅制或免费额度），跳过以免按 0 计费"
	}
	return "无法解析上游计费表达式（格式可能已变更）：" + err.Error()
}

func parseBillingExpression(expression string) ([]billingExprBreakdown, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, gerror.New("billing expression is empty")
	}
	breakdowns := make([]billingExprBreakdown, 0, 2)
	if err := walkBillingExpression(expression, billingExprBreakdown{}, &breakdowns); err != nil {
		return nil, err
	}
	if len(breakdowns) == 0 {
		return nil, gerror.New("billing expression produced no price branch")
	}
	return breakdowns, nil
}

// walkBillingExpression 递归展开三层三元表达式：命中分支继承外层上下文，
// 补集分支（else）只继承与自身维度无关的上层条件。
func walkBillingExpression(expression string, context billingExprBreakdown, output *[]billingExprBreakdown) error {
	expression = unwrapBillingParens(strings.TrimSpace(expression))
	condition, whenTrue, whenFalse, ok := splitBillingTernary(expression)
	if !ok {
		breakdown, err := parseBillingLeaf(expression)
		if err != nil {
			return err
		}
		breakdown.Timezone = context.Timezone
		breakdown.Weekdays = context.Weekdays
		breakdown.HourRanges = context.HourRanges
		breakdown.MinInput = context.MinInput
		breakdown.MaxInput = context.MaxInput
		breakdown.Note = context.Note
		*output = append(*output, breakdown)
		return nil
	}
	switch classifyBillingCondition(condition) {
	case billingExprConditionSchedule:
		schedule, err := parseBillingScheduleCondition(condition)
		if err != nil {
			return err
		}
		// 命中分支按解析出的时区 / 星期 / 时段落 conditions.time；else 分支是
		// 「不在该时段内」这一补集条件，无法用只支持「与」的条件结构表达，
		// 因此 else 一侧不带时段条件、只作兜底规则，靠入库顺序保证优先级：
		// orderSyncedRules 把无条件规则排在前（先入库 = id 更小），计费按 id
		// 降序试匹配，条件规则因此先于兜底规则命中，高峰价不会被兜底价盖掉。
		// 这里先展开 else 分支，让同一表达式内的兜底规则拿到更小的 id。
		fallback := context
		fallback.Timezone, fallback.Weekdays, fallback.HourRanges = "", nil, nil
		fallback.Note = "非高峰档"
		if err = walkBillingExpression(whenFalse, fallback, output); err != nil {
			return err
		}
		hit := context
		hit.Timezone, hit.Weekdays, hit.HourRanges = schedule.Timezone, schedule.Weekdays, schedule.HourRanges
		hit.Note = "高峰时段"
		return walkBillingExpression(whenTrue, hit, output)
	case billingExprConditionLength:
		bounds, err := parseBillingLengthCondition(condition)
		if err != nil {
			return err
		}
		hit := context
		hit.MinInput, hit.MaxInput = mergeBillingInputBounds(context.MinInput, context.MaxInput, bounds.WhenTrueMin, bounds.WhenTrueMax)
		if err = walkBillingExpression(whenTrue, hit, output); err != nil {
			return err
		}
		fallback := context
		fallback.MinInput, fallback.MaxInput = mergeBillingInputBounds(context.MinInput, context.MaxInput, bounds.WhenFalseMin, bounds.WhenFalseMax)
		return walkBillingExpression(whenFalse, fallback, output)
	case billingExprConditionParam:
		// 请求参数条件无法用价格规则表达：取 else 分支（参数未开启）计价并标注。
		role := "请求参数"
		if match := billingExprParamCondition.FindStringSubmatch(condition); match != nil && strings.TrimSpace(match[1]) != "" {
			role = strings.TrimSpace(match[1])
		}
		fallback := context
		fallback.Note = role + " 未开启档"
		return walkBillingExpression(whenFalse, fallback, output)
	default:
		return gerror.Newf("unsupported billing expression condition %q", truncateBillingExpr(condition, 80))
	}
}

func billingExprSyncedRule(model string, breakdown billingExprBreakdown) syncedRule {
	conditions := make(map[string]any, 2)
	if breakdown.Timezone != "" {
		block := map[string]any{"tz": breakdown.Timezone}
		if len(breakdown.Weekdays) > 0 {
			block["weekdays"] = breakdown.Weekdays
		}
		if len(breakdown.HourRanges) > 0 {
			ranges := make([][]string, 0, len(breakdown.HourRanges))
			for _, item := range breakdown.HourRanges {
				ranges = append(ranges, []string{item[0], item[1]})
			}
			block["ranges"] = ranges
		}
		conditions["time"] = block
	}
	if breakdown.MinInput != nil {
		conditions["inputTokensAtLeast"] = *breakdown.MinInput
	}
	if breakdown.MaxInput != nil {
		conditions["inputTokensAtMost"] = *breakdown.MaxInput
	}
	encodedConditions, _ := json.Marshal(conditions)
	encodedRates, _ := json.Marshal(breakdown.Rates)
	return syncedRule{
		Model:      model,
		Name:       billingExprRuleNameFor(breakdown),
		Currency:   "USD",
		Conditions: encodedConditions,
		Rates:      encodedRates,
	}
}

// billingExprRuleNameFor 给分档规则加上档位后缀，便于管理端区分；
// 无条件的标准档保持原有名称，与旧倍率格式的同步结果一致。
func billingExprRuleNameFor(breakdown billingExprBreakdown) string {
	suffix := breakdown.Note
	if suffix == "" && (breakdown.Timezone != "" || breakdown.MinInput != nil || breakdown.MaxInput != nil) {
		suffix = breakdown.Tier
	}
	if strings.TrimSpace(suffix) == "" {
		return billingExprRuleName
	}
	return fmt.Sprintf("%s（%s）", billingExprRuleName, suffix)
}

func truncateBillingExpr(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
