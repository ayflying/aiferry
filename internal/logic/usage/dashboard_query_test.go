package usage

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDashboardBucketShiftSecondsFollowsTimeZoneDifference(t *testing.T) {
	now := time.Now()
	_, localOffset := now.In(time.Local).Zone()
	_, utcOffset := now.In(time.UTC).Zone()
	if got, want := dashboardBucketShiftSeconds(time.UTC), utcOffset-localOffset; got != want {
		t.Fatalf("dashboardBucketShiftSeconds(UTC) = %d, want %d", got, want)
	}
	if got := dashboardBucketShiftSeconds(time.Local); got != 0 {
		t.Fatalf("dashboardBucketShiftSeconds(time.Local) = %d, want 0", got)
	}
}

func TestTrendBucketExpressionOnlyShiftsWhenNeeded(t *testing.T) {
	tests := []struct {
		name         string
		bucketUnit   string
		shiftSeconds int
		want         string
	}{
		{
			name:       "hour without shift keeps bare column",
			bucketUnit: trendBucketHour,
			want:       "DATE_FORMAT(created_at, '%Y-%m-%d %H:00:00')",
		},
		{
			name:         "hour with shift wraps column",
			bucketUnit:   trendBucketHour,
			shiftSeconds: 28800,
			want:         "DATE_FORMAT(FROM_UNIXTIME(UNIX_TIMESTAMP(created_at) + 28800), '%Y-%m-%d %H:00:00')",
		},
		{
			name:       "day without shift keeps bare column",
			bucketUnit: trendBucketDay,
			want:       "DATE_FORMAT(created_at, '%Y-%m-%d')",
		},
		{
			name:         "day with negative shift",
			bucketUnit:   trendBucketDay,
			shiftSeconds: -28800,
			want:         "DATE_FORMAT(FROM_UNIXTIME(UNIX_TIMESTAMP(created_at) + -28800), '%Y-%m-%d')",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := trendBucketExpression("created_at", test.bucketUnit, test.shiftSeconds); got != test.want {
				t.Fatalf("trendBucketExpression() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCostBucketExpressionMatchesGoBucketBoundaries(t *testing.T) {
	startLocal := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	tests := []struct {
		name string
		unit costBucketUnit
		want string
	}{
		{
			name: "hour truncates to wall clock hour",
			unit: costBucketHour,
			want: "DATE_FORMAT(created_at, '%Y-%m-%d %H:00:00')",
		},
		{
			name: "day truncates to wall clock day",
			unit: costBucketDay,
			want: "DATE_FORMAT(created_at, '%Y-%m-%d')",
		},
		{
			name: "week anchors on range start day every seven days",
			unit: costBucketWeek,
			want: "DATE_FORMAT(FROM_DAYS(TO_DAYS(DATE(created_at)) - MOD(DATEDIFF(DATE(created_at), '2026-07-01'), 7)), '%Y-%m-%d')",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := costBucketExpression("created_at", test.unit, startLocal, 0); got != test.want {
				t.Fatalf("costBucketExpression() = %q, want %q", got, test.want)
			}
		})
	}
	if got, want := costBucketExpression("created_at", costBucketDay, startLocal, 28800),
		"DATE_FORMAT(FROM_UNIXTIME(UNIX_TIMESTAMP(created_at) + 28800), '%Y-%m-%d')"; got != want {
		t.Fatalf("shifted costBucketExpression() = %q, want %q", got, want)
	}
}

// 周桶的 SQL 表达式必须与 Go 侧 dashboardDayCount 的整数除法偏移等价：
// 起始日与取值日之间的日历天数差按 7 取模后回退，即 days - days%7。
func TestCostBucketWeekOffsetMatchesGoIntegerDivision(t *testing.T) {
	rangeDay := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	for offset := 0; offset < 40; offset++ {
		value := rangeDay.AddDate(0, 0, offset)
		days := dashboardDayCount(rangeDay, value)
		want := rangeDay.AddDate(0, 0, days/7*7)
		// SQL 侧是从取值日回退 days%7 天，两者必须落在同一个桶。
		got := value.AddDate(0, 0, -(days % 7))
		if !got.Equal(want) {
			t.Fatalf("offset %d: MOD form gives %s, Go form gives %s", offset, got, want)
		}
	}
}

// 模型名分组必须区分大小写：库默认 collation（utf8mb4_0900_ai_ci）忽略大小写，
// 直接 GROUP BY requested_model 会把 Qwen3.8-Flash 与 qwen3.8-flash 合成一组，
// 而改动前用 Go map 分组是区分大小写的，两者展示结果不同。
func TestModelGroupingStaysCaseSensitive(t *testing.T) {
	if got, want := modelGroupExpression("requested_model"), "CAST(requested_model AS BINARY)"; got != want {
		t.Fatalf("modelGroupExpression() = %q, want %q", got, want)
	}
	if got, want := modelGroupNameExpression("requested_model"), "ANY_VALUE(requested_model) AS name"; got != want {
		t.Fatalf("modelGroupNameExpression() = %q, want %q", got, want)
	}
}

// bareGroupWord 复现 GoFrame gdb.quoteWordReg：只有纯标识符才会被 Group 加反引号。
var bareGroupWord = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// 分组表达式必须能安全通过 GoFrame Group 的改写。
//
// Group() 会对参数整体调用 QuoteString，因此写进 Group 的表达式不能在任何逗号段的
// 开头出现裸标识符（详见 checkGroupExpressionQuoting）。历史上踩过三次同一个坑：
// "BINARY requested_model"、"DATE_SUB(d, INTERVAL n DAY)"、"DATE_ADD(d, INTERVAL n SECOND)"。
func TestGroupExpressionsSurviveQuoteString(t *testing.T) {
	rangeStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	checkGroupExpressionQuoting(t, "模型分组", modelGroupExpression("requested_model"))
	checkGroupExpressionQuoting(t, "渠道分组", "COALESCE(channel_id, 0)")
	checkGroupExpressionQuoting(t, "小时桶", trendBucketExpression("created_at", trendBucketHour, 0))
	checkGroupExpressionQuoting(t, "天桶", trendBucketExpression("created_at", trendBucketDay, 0))
	checkGroupExpressionQuoting(t, "天桶(带时区偏移)", trendBucketExpression("created_at", trendBucketDay, 28800))
	checkGroupExpressionQuoting(t, "周桶", costBucketExpression("created_at", costBucketWeek, rangeStart, 0))
	checkGroupExpressionQuoting(t, "周桶(带时区偏移)", costBucketExpression("created_at", costBucketWeek, rangeStart, 28800))
}

// checkGroupExpressionQuoting 复现 Group 的 QuoteString 分段规则：先按逗号切段，
// 再取每段空格前的第一个词；该词若为纯标识符就会被套上反引号，使原本的关键字或
// 表达式片段变成列名（INTERVAL -> `INTERVAL`）而导致语法错误。
func checkGroupExpressionQuoting(t *testing.T, name, expression string) {
	t.Helper()
	for _, commaSegment := range strings.Split(expression, ",") {
		head := strings.TrimSpace(commaSegment)
		if index := strings.Index(head, " "); index >= 0 {
			head = head[:index]
		}
		if head == "" {
			continue
		}
		if bareGroupWord.MatchString(head) {
			t.Fatalf("%s 表达式 %q 逗号段的首词 %q 是裸标识符，会被 Group 加反引号破坏",
				name, expression, head)
		}
	}
}
