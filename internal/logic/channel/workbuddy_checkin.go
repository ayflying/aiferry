package channel

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
)

// WorkBuddy 每日签到（领取签到积分）。
//
// 接口取自官方桌面端的计费域：
//
//	POST {billingBase}/v2/billing/meter/daily-checkin   请求体 {}，返回签到结果
//
// 与「签到状态查询」的关键差异：状态查询（checkin-activity-status）要求
// WorkBuddy 自带 Turing 风控 SDK 生成的 X-Device-Token，该头无法在服务端复现，
// 所以本网关一直只能把它做成尽力而为；而签到领取接口只需
// Authorization: Bearer <accessToken>、Accept、Content-Type 三个头，服务端
// 可以正常复现，因此签到本身是可以自动完成的。
//
// 响应（code=0）：
//
//	{"code":0,"msg":"ok","data":{"credit":100,"streak_days":10,
//	   "is_streak_day":false}}
const workBuddyDailyCheckinPath = "/v2/billing/meter/daily-checkin"

type workBuddyCheckinClaimResponse struct {
	Code json.Number `json:"code"`
	Msg  string      `json:"msg"`
	Data struct {
		Credit      json.Number `json:"credit"`
		StreakDays  json.Number `json:"streak_days"`
		IsStreakDay bool        `json:"is_streak_day"`
	} `json:"data"`
}

// workBuddyCheckedInToday 判断签到状态响应里的 today_checked_in 是否为真。
// 只有明确读到 true 才当作「今天已签」；字段缺失或解析失败一律按「未知」返回
// false，让调用方走签到流程——上游对重复签到是幂等的（返回 code=0、credit=0），
// 代价远小于漏签一天。
func workBuddyCheckedInToday(body []byte) bool {
	return gjson.GetBytes(body, "data.today_checked_in").Bool()
}

// workBuddyCheckinClaim 是一次签到领取的结果，用于回写日志与额度提示。
type workBuddyCheckinClaim struct {
	Credit      float64
	StreakDays  float64
	IsStreakDay bool
}

// parseWorkBuddyCheckinClaim 解析 daily-checkin 的返回。上游对「今天已经签过」
// 的处理不统一（可能返回业务错误码，也可能返回 code=0 但 credit=0），这里只
// 把明确的业务错误当失败，其余按成功处理并由调用方回读状态确认。
func parseWorkBuddyCheckinClaim(body []byte) (workBuddyCheckinClaim, error) {
	var payload workBuddyCheckinClaimResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return workBuddyCheckinClaim{}, gerror.Wrap(err, "decode workbuddy checkin claim response")
	}
	if payload.Code != "" && payload.Code.String() != "0" && payload.Code.String() != "200" {
		message := strings.TrimSpace(payload.Msg)
		if message == "" {
			message = "上游未返回 WorkBuddy 签到结果"
		}
		return workBuddyCheckinClaim{}, gerror.New("WorkBuddy 签到失败：" + message)
	}
	return workBuddyCheckinClaim{
		Credit:      workBuddyNumber(payload.Data.Credit),
		StreakDays:  workBuddyNumber(payload.Data.StreakDays),
		IsStreakDay: payload.Data.IsStreakDay,
	}, nil
}

// workBuddyCheckinText 把签到结果整理成一句可读提示，写进额度视图的
// PartialErrors（该字段同时承载提示文案）。
func workBuddyCheckinText(prefix string, claim workBuddyCheckinClaim) string {
	var builder strings.Builder
	builder.WriteString("密钥 ")
	builder.WriteString(prefix)
	builder.WriteString("：已自动签到")
	if claim.Credit > 0 {
		builder.WriteString("，获得 ")
		builder.WriteString(workBuddyNumberText(claim.Credit))
		builder.WriteString(" 积分")
	}
	if claim.StreakDays > 0 {
		builder.WriteString("，连续 ")
		builder.WriteString(workBuddyNumberText(claim.StreakDays))
		builder.WriteString(" 天")
	}
	return builder.String()
}

// workBuddyNumberText 把积分/天数渲染成不带多余小数的文本。
func workBuddyNumberText(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "0"
	}
	return string(raw)
}
