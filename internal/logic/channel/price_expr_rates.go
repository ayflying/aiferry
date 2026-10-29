package channel

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// 本文件负责表达式叶子里的费率项：tier("<档名>", <线性表达式>) 的第二段。

const (
	// billingExprFixedVariable 是 fixed(x) 项在中间结果里的占位变量名。
	billingExprFixedVariable = "fixed"
)

// errBillingExprNoPrice 表示表达式合法但不构成价格：上游对订阅制、免费额度或
// 独占渠道的模型会写成 p * 0 + c * 0。这类条目不等于「解析失败」，也绝不能
// 落库成 0 价规则——否则模型会被判成已定价却永远按 0 计费，等于对所有人免费。
// 用哨兵错误区分二者，好让同步过程给出「上游就是不计费」而非「格式变了」的提示。
var errBillingExprNoPrice = gerror.New("billing expression prices nothing")

func isBillingExprNoPriceError(err error) bool {
	return errors.Is(err, errBillingExprNoPrice)
}

// billingExprRateKeys 把表达式变量映射到价格规则的费率字段。
var billingExprRateKeys = map[string]string{
	"p":  "inputPerMillion",
	"c":  "outputPerMillion",
	"cr": "cachedInputPerMillion",
	"cc": "cacheWritePerMillion",
	"ai": "audioInputPerMillion",
	"ao": "audioOutputPerMillion",
}

// billingExprCacheWriteFallbacks 是高 TTL 缓存写入价变量，按 TTL 从短到长排列：
// cc1h 是 1 小时档、cc12h 是 12 小时档。AiFerry 只有一个缓存写入费率字段，
// 因此只在 cc（短 TTL）缺席时按该顺序取第一个可用值兜底，避免高 TTL 价覆盖
// 低 TTL 档。cc12h 是上游 2026-10 新增的变量，OpenAI 新一代模型（gpt-5.6 /
// gpt-6 / gpt-daybreak 等）的表达式全都引用它；不认识它会让这些模型整条丢掉。
var billingExprCacheWriteFallbacks = []string{"cc1h", "cc12h"}

// billingExprImageTokensPerPicture 曾经用于把「每张图 x 美元」折算成图像输入
// tokens。该折算已废弃：图像生成端点（/images/generations）的响应里没有 usage，
// AiFerry 拿不到图像 tokens，规则因此永远算不出金额——模型会被判成「已定价」
// 却算不出费用，请求直接以「上游未返回可计费用的用量信息」失败。按张计费统一
// 落成按次费率，与上游 model_price 字段及既有的按次图像计费口径一致。
// 已知局限：n > 1 的一次请求仍按一份计价（与原按次价格源同一限制）。

var (
	billingExprLeafPattern  = regexp.MustCompile(`^tier\("([^"]*)"\s*,\s*(.+)\)$`)
	billingExprVariableTerm = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*)\s*\*\s*([0-9]*\.?[0-9]+)$`)
	billingExprNumberTerm   = regexp.MustCompile(`^([0-9]*\.?[0-9]+)\s*\*\s*([A-Za-z][A-Za-z0-9_]*)$`)
	billingExprFixedTerm    = regexp.MustCompile(`^fixed\(([0-9]*\.?[0-9]+)\)$`)
	// billingExprImageCountSuffix 匹配按张计费表达式的尾部「* image_count」。
	// 该后缀在解析前摘掉：金额按每张图计，落库时按次计价（见上方说明）。
	billingExprImageCountSuffix = regexp.MustCompile(`\s*\*\s*image_count\s*$`)
)

// parseBillingLeaf 解析 tier("<档名>", <线性表达式>) 叶子。
// 按张计费表达式在 tier(...) 之后再乘 image_count，先摘掉这个后缀才能匹配到
// tier 结构；金额本身按每张图计，落库时按次计价（理由见文件头注释）。
func parseBillingLeaf(expression string) (billingExprBreakdown, error) {
	expression = strings.TrimSpace(expression)
	expression = strings.TrimSpace(billingExprImageCountSuffix.ReplaceAllString(expression, ""))
	match := billingExprLeafPattern.FindStringSubmatch(expression)
	if match == nil {
		return billingExprBreakdown{}, gerror.Newf("unsupported billing expression leaf %q", truncateBillingExpr(expression, 80))
	}
	rates, err := parseBillingLinearRates(match[2])
	if err != nil {
		return billingExprBreakdown{}, err
	}
	return billingExprBreakdown{Tier: match[1], Rates: rates}, nil
}

// parseBillingLinearRates 解析形如 p * 3 + cr * 0.3 + cc * 5 + cc12h * 10 + fixed(0.04)
// 的线性表达式：逐项累加同一变量的系数，出现未知变量即失败。
//
// 上游有两种写法需要额外处理：
//   - 系数全为 0 的表达式（tier("standard", p * 0 + cr * 0 + c * 0)）表示该
//     模型走订阅制或免费额度、不按量计费。这类条目必须判为「无价格」跳过，
//     否则会落成 0 价规则：模型被判成已定价，却永远按 0 计费。
//   - cc1h / cc12h 两个高 TTL 缓存写入价，见 billingExprCacheWriteFallbacks。
func parseBillingLinearRates(expression string) (map[string]float64, error) {
	expression = strings.TrimSpace(expression)
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
	return billingExprRatesFromTerms(terms)
}

// billingExprRatesFromTerms 把变量系数换算成价格规则费率。
func billingExprRatesFromTerms(terms map[string]float64) (map[string]float64, error) {
	rates := make(map[string]float64, len(terms))
	for variable, value := range terms {
		if !isBillingExprKnownVariable(variable) {
			return nil, gerror.Newf("unsupported billing expression variable %q", variable)
		}
		if value == 0 {
			// 0 系数不构成价格：上游用 p * 0 表示该维度不计费，不能因此生成 0 价。
			continue
		}
		switch variable {
		case billingExprFixedVariable:
			// fixed(x) 是每次请求固定价。按张计费（* image_count）也落到这里：
			// 图像生成响应没有 usage，折算成图像 tokens 会让规则永远算不出金额。
			rates["request"] += value
		case "cc1h", "cc12h":
			// 高 TTL 缓存写入价在下面按 TTL 顺序兜底，这里不直接落费率。
		default:
			rates[billingExprRateKeys[variable]] += value
		}
	}
	if len(rates) == 0 {
		return nil, errBillingExprNoPrice
	}
	if _, exists := rates["cacheWritePerMillion"]; !exists {
		for _, variable := range billingExprCacheWriteFallbacks {
			if value := terms[variable]; value != 0 {
				rates["cacheWritePerMillion"] = value
				break
			}
		}
	}
	return rates, nil
}

// isBillingExprKnownVariable 判断变量是否在已知费率维度内。
func isBillingExprKnownVariable(variable string) bool {
	if _, exists := billingExprRateKeys[variable]; exists {
		return true
	}
	if variable == billingExprFixedVariable {
		return true
	}
	for _, fallback := range billingExprCacheWriteFallbacks {
		if variable == fallback {
			return true
		}
	}
	return false
}

func parseBillingCoefficient(raw, term string) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, gerror.Newf("billing expression term %q has an invalid coefficient", term)
	}
	return value, nil
}
