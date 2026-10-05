package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// WorkBuddy（腾讯 CodeBuddy 系桌面客户端）没有面向第三方的开放接口文档，
// 这里的积分口径取自桌面端自身的用量面板。桌面端把额度查询拆成两个接口：
//
//	POST {host}/billing/meter/get-user-resource-summary   积分余额（仅 Authorization 头）
//	POST {host}/billing/meter/checkin-activity-status    签到状态（额外需 X-Device-Token）
//
// 前者只需 Authorization: Bearer <accessToken>；后者还要求 WorkBuddy 自带
// Turing 风控 SDK 生成的 X-Device-Token，该头无法在服务端复现，因此本适配器
// 把签到查询作为尽力而为的第二请求，失败时降级为 PartialErrors 提示。
//
// 积分响应（code=0）：
//
//	{"code":0,"msg":"ok","data":{"Packages":[
//	   {"PackageCode":"...","CycleTotalCapacity":3000,
//	    "CycleRemainCapacity":1500,"CycleUsedCapacity":1500}],
//	  "SubscriptionPackageCode":"...","IsPaidUser":true}}
//
// 签到响应（code=0）：
//
//	{"code":0,"msg":"ok","data":{"active":true,"today_checked_in":true,
//	   "streak_days":9,"is_streak_day":false,"next_streak_day":0,
//	   "today_credit":100,"streak_bonus_days":0,"streak_bonus_credit":0}}

// workBuddyCreditsPath 是桌面端资源汇总接口的路径（host 根，无 /v2 前缀）。
// workBuddyCheckinPath 是桌面端签到状态接口的路径（/v2 前缀由渠道 baseUrl 提供）。
const (
	workBuddyCreditsPath = "/billing/meter/get-user-resource-summary"
	workBuddyCheckinPath = "/billing/meter/checkin-activity-status"
	workBuddyCreditsKind = "monthly"
	workBuddyCheckinKind = "checkin"
)

type workBuddySummaryResponse struct {
	Code json.Number            `json:"code"`
	Msg  string                 `json:"msg"`
	Data workBuddySummaryData   `json:"data"`
}

type workBuddySummaryData struct {
	Packages                []workBuddyPackage `json:"Packages"`
	SubscriptionPackageCode string             `json:"SubscriptionPackageCode"`
	IsPaidUser              bool               `json:"IsPaidUser"`
}

type workBuddyPackage struct {
	PackageCode         string      `json:"PackageCode"`
	CycleTotalCapacity  json.Number `json:"CycleTotalCapacity"`
	CycleRemainCapacity json.Number `json:"CycleRemainCapacity"`
	CycleUsedCapacity   json.Number `json:"CycleUsedCapacity"`
}

type workBuddyCheckinResponse struct {
	Code json.Number `json:"code"`
	Msg  string      `json:"msg"`
	Data struct {
		Active         bool        `json:"active"`
		TodayCheckedIn bool        `json:"today_checked_in"`
		StreakDays     json.Number `json:"streak_days"`
		TodayCredit    json.Number `json:"today_credit"`
	} `json:"data"`
}

// parseWorkBuddyCredits 解析 get-user-resource-summary，把每个有效套餐映射成
// 一个 monthly 窗口（桌面端把积分按订阅周期滚动结算，月度窗口语义最贴合）。
// 已用 = 上游 CycleUsedCapacity；缺失时用 总 − 剩余 补齐。
func parseWorkBuddyCredits(body []byte) (QuotaView, error) {
	var payload workBuddySummaryResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return QuotaView{}, gerror.Wrap(err, "decode workbuddy quota response")
	}
	if payload.Code != "" && payload.Code.String() != "0" && payload.Code.String() != "200" {
		message := strings.TrimSpace(payload.Msg)
		if message == "" {
			message = "上游未返回 WorkBuddy 积分数据"
		}
		return QuotaView{}, gerror.New("WorkBuddy 积分查询失败：" + message)
	}
	view := QuotaView{Mode: channeltype.AdapterWorkBuddy, QueriedAt: time.Now()}
	for _, item := range payload.Data.Packages {
		packageTotal := workBuddyNumber(item.CycleTotalCapacity)
		if packageTotal <= 0 {
			continue
		}
		packageRemain := workBuddyNumber(item.CycleRemainCapacity)
		used := workBuddyNumber(item.CycleUsedCapacity)
		if used <= 0 {
			used = max(packageTotal-packageRemain, 0)
		}
		remaining := min(max(packageRemain, 0), packageTotal)
		percent := used / packageTotal * 100
		window := QuotaWindow{
			Kind:        workBuddyCreditsKind,
			Label:       workBuddyPackageLabel(item.PackageCode),
			UsedPercent: percent,
			Used:        &used,
			Total:       &packageTotal,
			Remaining:   &remaining,
		}
		view.Windows = append(view.Windows, window)
	}
	if len(view.Windows) == 0 {
		return QuotaView{}, gerror.Newf("WorkBuddy 未返回有效的积分额度窗口，原始响应：%s", quotaRawSnippet(body))
	}
	view.Level = workBuddyLevel(payload.Data.SubscriptionPackageCode, payload.Data.IsPaidUser)
	return view, nil
}

// parseWorkBuddyCheckin 解析 checkin-activity-status，生成一条 checkin 窗口：
// 已用百分比 = 已签到则 100、未签到则 0；used = 累计连续签到天数；total = 今日签到积分。
func parseWorkBuddyCheckin(body []byte) (QuotaView, error) {
	var payload workBuddyCheckinResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return QuotaView{}, gerror.Wrap(err, "decode workbuddy checkin response")
	}
	if payload.Code != "" && payload.Code.String() != "0" {
		message := strings.TrimSpace(payload.Msg)
		if message == "" {
			message = "上游未返回 WorkBuddy 签到状态"
		}
		return QuotaView{}, gerror.New("WorkBuddy 签到状态查询失败：" + message)
	}
	streak := workBuddyNumber(payload.Data.StreakDays)
	credit := workBuddyNumber(payload.Data.TodayCredit)
	percent := 0.0
	if payload.Data.TodayCheckedIn {
		percent = 100
	}
	view := QuotaView{
		Mode:      channeltype.AdapterWorkBuddy,
		Level:     "签到",
		QueriedAt: time.Now(),
		Windows: []QuotaWindow{
			{
				Kind:        workBuddyCheckinKind,
				Label:       "每日签到",
				UsedPercent: percent,
				Used:        &streak,
				Total:       &credit,
				Remaining:   &credit,
			},
		},
	}
	return view, nil
}

func workBuddyPackageLabel(packageCode string) string {
	if packageCode == "" {
		return "积分额度"
	}
	return packageCode
}

func workBuddyLevel(subscriptionCode string, isPaidUser bool) string {
	if subscriptionCode == "" {
		if isPaidUser {
			return "付费用户"
		}
		return "免费用户"
	}
	return subscriptionCode
}

func workBuddyNumber(value json.Number) float64 {
	if value == "" {
		return 0
	}
	f, err := value.Float64()
	if err != nil {
		return 0
	}
	return f
}

// fetchWorkBuddyCredits 走渠道推理密钥并发查 WorkBuddy 积分，并对每个成功
// 密钥尽力查一次签到状态。签到失败不阻塞积分，降级为 PartialErrors。
// 与 fetchQuota 不同：积分接口在 host 根（resourcePrefix 为空），签到接口在
// baseUrl（含 /v2）下，二者路径由常量定义，不再依赖配置里 quota.path。
func (s *sChannel) fetchWorkBuddyCredits(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig) (QuotaView, error) {
	creditsEndpoint, err := resolveHostURL(channel.BaseUrl, workBuddyCreditsPath)
	if err != nil {
		return QuotaView{}, err
	}
	checkinEndpoint, err := resolveHostURL(channel.BaseUrl, "/v2"+workBuddyCheckinPath)
	if err != nil {
		return QuotaView{}, err
	}
	credentials, err := s.credentialCiphers(ctx, channel.Id)
	if err != nil {
		return QuotaView{}, err
	}
	views := make([]QuotaView, len(credentials))
	failed := make([]string, len(credentials))
	var wg sync.WaitGroup
	for index, credential := range credentials {
		wg.Add(1)
		go func(index int, credential quotaCredential) {
			defer wg.Done()
			credits, creditsErr := s.fetchWorkBuddyCreditsSingle(ctx, channel, config, credential, creditsEndpoint, checkinEndpoint)
			if creditsErr != nil {
				failed[index] = creditsErr.Error()
				return
			}
			views[index] = credits
		}(index, credential)
	}
	wg.Wait()
	success := make([]QuotaView, 0, len(credentials))
	failures := make([]string, 0, len(credentials))
	for index := range credentials {
		if failed[index] != "" {
			failures = append(failures, fmt.Sprintf("密钥 %s：%s", credentials[index].Prefix, failed[index]))
			continue
		}
		success = append(success, views[index])
	}
	if len(success) == 0 {
		return QuotaView{}, gerror.New(strings.Join(failures, "；"))
	}
	view := mergeQuotaViews(success)
	view.PartialErrors = failures
	return view, nil
}

// fetchWorkBuddyCreditsSingle 先查积分，成功后再尽力查签到；签到失败写入
// view.PartialErrors，不向上抛错（避免把一次风控头缺失的降级也当成积分失败）。
func (s *sChannel) fetchWorkBuddyCreditsSingle(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig, credential quotaCredential, creditsEndpoint, checkinEndpoint string) (QuotaView, error) {
	creditsBody, err := s.fetchUpstreamJSON(ctx, channel, credential.Cipher, upstreamJSONRequest{
		Method:       "POST",
		Endpoint:     creditsEndpoint,
		AuthType:     config.AuthType,
		HeaderName:   config.HeaderName,
		HeaderPrefix: "Bearer ",
		BodyLimit:    1 << 20,
		RequestError: "创建 WorkBuddy 积分查询请求失败",
		FetchError:   "请求 WorkBuddy 积分接口失败",
		ReadError:    "读取 WorkBuddy 积分接口响应失败",
		InvalidError: "WorkBuddy 积分接口返回了无效 JSON",
		StatusError: func(status int, payload []byte) error {
			return gerror.Newf("WorkBuddy 积分接口返回 HTTP %d", status)
		},
	})
	if err != nil {
		return QuotaView{}, err
	}
	view, err := parseWorkBuddyCredits(creditsBody)
	if err != nil {
		return QuotaView{}, err
	}
	checkinBody, checkinErr := s.fetchUpstreamJSON(ctx, channel, credential.Cipher, upstreamJSONRequest{
		Method:       "POST",
		Endpoint:     checkinEndpoint,
		AuthType:     config.AuthType,
		HeaderName:   config.HeaderName,
		HeaderPrefix: "Bearer ",
		BodyLimit:    1 << 20,
		RequestError: "创建 WorkBuddy 签到查询请求失败",
		FetchError:   "请求 WorkBuddy 签到接口失败",
		ReadError:    "读取 WorkBuddy 签到接口响应失败",
		InvalidError: "WorkBuddy 签到接口返回了无效 JSON",
		StatusError: func(status int, payload []byte) error {
			return gerror.Newf("WorkBuddy 签到接口返回 HTTP %d", status)
		},
	})
	if checkinErr == nil {
		if checkinView, parseErr := parseWorkBuddyCheckin(checkinBody); parseErr == nil {
			view.Windows = append(view.Windows, checkinView.Windows...)
		} else {
			view.PartialErrors = append(view.PartialErrors, fmt.Sprintf("密钥 %s：签到状态解析失败：%s", credential.Prefix, parseErr.Error()))
		}
	} else {
		view.PartialErrors = append(view.PartialErrors, fmt.Sprintf("密钥 %s：签到状态查询失败（需 X-Device-Token 风控头）：%s", credential.Prefix, checkinErr.Error()))
	}
	return view, nil
}
