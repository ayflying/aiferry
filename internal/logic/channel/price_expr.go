package channel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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
//	cr 缓存读取价          cc 缓存写入价        cc1h 1 小时缓存写入价
//	ai 音频输入价          ao 音频输出价        fixed(x) 每次请求固定价
//
// 这里把它翻译成 AiFerry 的价格规则：时段映射到 conditions.time（上游用
// UTC），上下文长度映射到 inputTokensAtMost / inputTokensAtLeast。思考模式
// 依赖请求体参数（param("enable_thinking")），网关没有对应的规则条件，
// 只能取 else 分支（未开启思考）计价，并在规则名里标注。

const (
	billingExprTieredMode = "tiered_expr"
	billingExprRuleName   = "BaseLLM 官方模型价格"
	// billingExprFixedVariable 是 fixed(x) 项在中间结果里的占位变量名。
	billingExprFixedVariable = "fixed"
)

// billingExprRateKeys 把表达式变量映射到价格规则的费率字段。
// cc1h（1 小时 TTL 缓存写入价）在 AiFerry 没有独立字段，仅在缺少 cc 时
// 回退成缓存写入价。
var billingExprRateKeys = map[string]string{
	"p":  "inputPerMillion",
	"c":  "outputPerMillion",
	"cr": "cachedInputPerMillion",
	"cc": "cacheWritePerMillion",
	"ai": "audioInputPerMillion",
	"ao": "audioOutputPerMillion",
}

var (
	billingExprLeafPattern      = regexp.MustCompile(`^tier\("([^"]*)"\s*,\s*(.+)\)$`)
	billingExprVariableTerm     = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*)\s*\*\s*([0-9]*\.?[0-9]+)$`)
	billingExprNumberTerm       = regexp.MustCompile(`^([0-9]*\.?[0-9]+)\s*\*\s*([A-Za-z][A-Za-z0-9_]*)$`)
	billingExprFixedTerm        = regexp.MustCompile(`^fixed\(([0-9]*\.?[0-9]+)\)$`)
	billingExprLengthCondition  = regexp.MustCompile(`^len\s*(<=|<|>=|>)\s*([0-9]+)$`)
	billingExprParamCondition   = regexp.MustCompile(`^param\("([^"]*)"\)\s*==\s*(.+)$`)
	billingExprTimezonePattern  = regexp.MustCompile(`(?:weekday|hour)\("([^"]+)"\)`)
	billingExprWeekdayCondition = regexp.MustCompile(`weekday\("[^"]*"\)\s*(>=|<=|==)\s*([0-9]+)`)
	billingExprHourCondition    = regexp.MustCompile(`hour\("[^"]*"\)\s*(>=|>)\s*([0-9]+)\s*&&\s*hour\("[^"]*"\)\s*(<=|<)\s*([0-9]+)`)
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
func syncedRulesFromBillingExpressions(expressions, modes gjson.Result) ([]syncedRule, error) {
	if !expressions.IsObject() {
		return nil, gerror.New("NewAPI billing expression source did not return an object")
	}
	rules := make([]syncedRule, 0, len(expressions.Map())+8)
	skipped := 0
	expressions.ForEach(func(model, expression gjson.Result) bool {
		name := strings.TrimSpace(model.String())
		if name == "" {
			return true
		}
		if mode := strings.TrimSpace(modes.Get(name).String()); mode != "" && mode != billingExprTieredMode {
			skipped++
			return true
		}
		breakdowns, err := parseBillingExpression(expression.String())
		if err != nil {
			skipped++
			return true
		}
		for _, breakdown := range breakdowns {
			rules = append(rules, billingExprSyncedRule(name, breakdown))
		}
		return true
	})
	if len(rules) == 0 {
		return nil, gerror.Newf("NewAPI billing expression source had no usable priced model (%d skipped)", skipped)
	}
	return rules, nil
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

type billingExprConditionKind int

const (
	billingExprConditionUnknown billingExprConditionKind = iota
	billingExprConditionSchedule
	billingExprConditionLength
	billingExprConditionParam
)

func classifyBillingCondition(condition string) billingExprConditionKind {
	condition = strings.TrimSpace(condition)
	switch {
	case strings.Contains(condition, "weekday(") || strings.Contains(condition, "hour("):
		return billingExprConditionSchedule
	case strings.HasPrefix(condition, "len"):
		return billingExprConditionLength
	case strings.HasPrefix(condition, "param("):
		return billingExprConditionParam
	default:
		return billingExprConditionUnknown
	}
}

// splitBillingTernary 按顶层（括号外、字符串外）的 ? 与 : 切开三元表达式。
func splitBillingTernary(expression string) (condition, whenTrue, whenFalse string, ok bool) {
	depth, question := 0, -1
	inString := false
	for index := 0; index < len(expression); index++ {
		character := expression[index]
		if inString {
			if character == '\\' {
				index++
				continue
			}
			if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '(':
			depth++
		case ')':
			depth--
		case '?':
			if depth == 0 && question < 0 {
				question = index
			}
		case ':':
			if depth == 0 && question >= 0 {
				return strings.TrimSpace(expression[:question]),
					strings.TrimSpace(expression[question+1 : index]),
					strings.TrimSpace(expression[index+1:]), true
			}
		}
	}
	return "", "", "", false
}

// unwrapBillingParens 去掉整体包裹的一层或多层括号，便于按顶层切分。
func unwrapBillingParens(expression string) string {
	for len(expression) >= 2 && expression[0] == '(' && expression[len(expression)-1] == ')' {
		if !billingExprOuterParensWrap(expression) {
			break
		}
		expression = strings.TrimSpace(expression[1 : len(expression)-1])
	}
	return expression
}

func billingExprOuterParensWrap(expression string) bool {
	depth := 0
	for index := 0; index < len(expression); index++ {
		switch expression[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && index != len(expression)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// parseBillingLeaf 解析 tier("<档名>", <线性表达式>) 叶子。
func parseBillingLeaf(expression string) (billingExprBreakdown, error) {
	match := billingExprLeafPattern.FindStringSubmatch(strings.TrimSpace(expression))
	if match == nil {
		return billingExprBreakdown{}, gerror.Newf("unsupported billing expression leaf %q", truncateBillingExpr(expression, 80))
	}
	rates, err := parseBillingLinearRates(match[2])
	if err != nil {
		return billingExprBreakdown{}, err
	}
	return billingExprBreakdown{Tier: match[1], Rates: rates}, nil
}

// parseBillingLinearRates 解析形如 p * 3 + cr * 0.3 + cc1h * 6 + fixed(0.04)
// 的线性表达式：逐项累加同一变量的系数，出现未知变量即失败。
func parseBillingLinearRates(expression string) (map[string]float64, error) {
	terms := make(map[string]float64)
	for _, raw := range strings.Split(expression, "+") {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		if match := billingExprVariableTerm.FindStringSubmatch(term); match != nil {
			value, err := parseBillingCoefficient(match[2], term)
			if err != nil {
				return nil, err
			}
			terms[match[1]] += value
			continue
		}
		if match := billingExprNumberTerm.FindStringSubmatch(term); match != nil {
			value, err := parseBillingCoefficient(match[1], term)
			if err != nil {
				return nil, err
			}
			terms[match[2]] += value
			continue
		}
		if match := billingExprFixedTerm.FindStringSubmatch(term); match != nil {
			value, err := parseBillingCoefficient(match[1], term)
			if err != nil {
				return nil, err
			}
			terms[billingExprFixedVariable] += value
			continue
		}
		return nil, gerror.Newf("unsupported billing expression term %q", term)
	}
	if len(terms) == 0 {
		return nil, gerror.New("billing expression has no priced term")
	}
	rates := make(map[string]float64, len(terms))
	for variable, value := range terms {
		if key, exists := billingExprRateKeys[variable]; exists {
			rates[key] += value
			continue
		}
		switch variable {
		case "cc1h":
			// 1 小时缓存写入价只在没有缓存写入价时兜底，避免覆盖 5 分钟档。
			if _, hasCacheWrite := terms["cc"]; !hasCacheWrite {
				rates["cacheWritePerMillion"] += value
			}
		case billingExprFixedVariable:
			rates["request"] += value
		default:
			return nil, gerror.Newf("unsupported billing expression variable %q", variable)
		}
	}
	return rates, nil
}

func parseBillingCoefficient(raw, term string) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, gerror.Newf("billing expression term %q has an invalid coefficient", term)
	}
	return value, nil
}

// billingExprLengthBounds 表示 len 比较命中分支与补集分支各自的输入长度区间。
type billingExprLengthBounds struct {
	WhenTrueMin  *uint64
	WhenTrueMax  *uint64
	WhenFalseMin *uint64
	WhenFalseMax *uint64
}

func parseBillingLengthCondition(condition string) (billingExprLengthBounds, error) {
	match := billingExprLengthCondition.FindStringSubmatch(strings.TrimSpace(condition))
	if match == nil {
		return billingExprLengthBounds{}, gerror.Newf("unsupported billing expression length condition %q", truncateBillingExpr(condition, 80))
	}
	bound, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil || bound == 0 {
		return billingExprLengthBounds{}, gerror.Newf("billing expression length condition %q is out of range", condition)
	}
	switch match[1] {
	case "<=":
		return billingExprLengthBounds{WhenTrueMax: uint64Pointer(bound), WhenFalseMin: uint64Pointer(bound + 1)}, nil
	case "<":
		return billingExprLengthBounds{WhenTrueMax: uint64Pointer(bound - 1), WhenFalseMin: uint64Pointer(bound)}, nil
	case ">=":
		return billingExprLengthBounds{WhenTrueMin: uint64Pointer(bound), WhenFalseMax: uint64Pointer(bound - 1)}, nil
	default: // ">"
		return billingExprLengthBounds{WhenTrueMin: uint64Pointer(bound + 1), WhenFalseMax: uint64Pointer(bound)}, nil
	}
}

// mergeBillingInputBounds 叠加多层长度区间：下界取更大值，上界取更小值。
func mergeBillingInputBounds(minimum, maximum, lower, upper *uint64) (*uint64, *uint64) {
	resultMin, resultMax := minimum, maximum
	if lower != nil && (resultMin == nil || *lower > *resultMin) {
		resultMin = lower
	}
	if upper != nil && (resultMax == nil || *upper < *resultMax) {
		resultMax = upper
	}
	return resultMin, resultMax
}

func parseBillingScheduleCondition(condition string) (billingExprBreakdown, error) {
	match := billingExprTimezonePattern.FindStringSubmatch(condition)
	if match == nil {
		return billingExprBreakdown{}, gerror.Newf("billing expression schedule condition %q has no timezone", truncateBillingExpr(condition, 80))
	}
	ranges, err := parseBillingHourRanges(condition)
	if err != nil {
		return billingExprBreakdown{}, err
	}
	return billingExprBreakdown{Timezone: match[1], Weekdays: parseBillingWeekdays(condition), HourRanges: ranges}, nil
}

// parseBillingWeekdays 把 weekday(tz) >= a && weekday(tz) <= b 折算成 ISO 编号列表。
func parseBillingWeekdays(condition string) []int {
	matches := billingExprWeekdayCondition.FindAllStringSubmatch(condition, -1)
	if len(matches) == 0 {
		return nil
	}
	lower, upper, exact := 0, 0, 0
	for _, match := range matches {
		value, err := strconv.Atoi(match[2])
		if err != nil || value < 0 || value > 7 {
			continue
		}
		switch match[1] {
		case ">=":
			lower = value
		case "<=":
			upper = value
		case "==":
			exact = value
		}
	}
	if exact > 0 {
		return []int{exact}
	}
	if lower == 0 && upper == 0 {
		return nil
	}
	if lower == 0 {
		lower = 1
	}
	if upper == 0 {
		upper = 7
	}
	if lower > upper {
		return nil
	}
	weekdays := make([]int, 0, upper-lower+1)
	for day := lower; day <= upper; day++ {
		weekdays = append(weekdays, day)
	}
	return weekdays
}

// parseBillingHourRanges 把 hour(tz) >= a && hour(tz) < b 的每个子句折算成
// "<HH:00>"-"<HH:00>" 区间（结束不含，24:00 表示一天结束）。
func parseBillingHourRanges(condition string) ([][2]string, error) {
	if !strings.Contains(condition, "hour(") {
		return nil, nil
	}
	matches := billingExprHourCondition.FindAllStringSubmatch(condition, -1)
	if len(matches) == 0 {
		return nil, gerror.Newf("unsupported billing expression hour condition %q", truncateBillingExpr(condition, 80))
	}
	ranges := make([][2]string, 0, len(matches))
	for _, match := range matches {
		start, startErr := strconv.Atoi(match[2])
		end, endErr := strconv.Atoi(match[4])
		if startErr != nil || endErr != nil || start < 0 || start > 24 || end < 0 || end > 24 {
			return nil, gerror.Newf("billing expression hour condition %q is out of range", condition)
		}
		if match[3] == "<=" {
			end++
		}
		if end > 24 {
			end = 24
		}
		if start >= end {
			continue
		}
		ranges = append(ranges, [2]string{fmt.Sprintf("%02d:00", start), fmt.Sprintf("%02d:00", end)})
	}
	if len(ranges) == 0 {
		return nil, gerror.Newf("billing expression hour condition %q covers no hour", truncateBillingExpr(condition, 80))
	}
	return ranges, nil
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

func uint64Pointer(value uint64) *uint64 {
	return &value
}

func truncateBillingExpr(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
