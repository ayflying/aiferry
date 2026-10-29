package channel

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

// 本文件负责表达式里的条件：时段（weekday / hour）、上下文长度（len）与
// 请求参数（param），以及三元表达式的顶层切分。

type billingExprConditionKind int

const (
	billingExprConditionUnknown billingExprConditionKind = iota
	billingExprConditionSchedule
	billingExprConditionLength
	billingExprConditionParam
)

var (
	billingExprLengthCondition  = regexp.MustCompile(`^len\s*(<=|<|>=|>)\s*([0-9]+)$`)
	billingExprParamCondition   = regexp.MustCompile(`^param\("([^"]*)"\)\s*==\s*(.+)$`)
	billingExprTimezonePattern  = regexp.MustCompile(`(?:weekday|hour)\("([^"]+)"\)`)
	billingExprWeekdayCondition = regexp.MustCompile(`weekday\("[^"]*"\)\s*(>=|<=|==)\s*([0-9]+)`)
	billingExprHourCondition    = regexp.MustCompile(`hour\("[^"]*"\)\s*(>=|>)\s*([0-9]+)\s*&&\s*hour\("[^"]*"\)\s*(<=|<)\s*([0-9]+)`)
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

func uint64Pointer(value uint64) *uint64 {
	return &value
}
