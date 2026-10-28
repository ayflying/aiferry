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

// WorkBuddy 积分使用 host 根的资源汇总接口，与 /v2 下的签到路由分开。
// 官网实际返回 data.Packages 和镜像 CreditGroups，解析只累计一份权威明细。
// 渠道密钥是否被上游接受需真实请求验证，不能从签到接口或 HTTP 401 推断。
//
// 签到响应（code=0）：
//
//	{"code":0,"msg":"ok","data":{"active":true,"today_checked_in":true,
//	   "streak_days":9,"is_streak_day":false,"next_streak_day":0,
//	   "today_credit":100,"streak_bonus_days":0,"streak_bonus_credit":0}}

// workBuddyCreditsPath 恢复官网实际使用、早期适配器使用的 host 根 summary 路由。
// 不能根据另一路由返回 401 而非 404，推断它提供同样的积分业务。
// workBuddyCheckinPath 保持原签到路径，/v2 前缀仍由调用方补齐。
const (
	workBuddyCreditsPath = "/billing/meter/get-user-resource-summary"
	workBuddyCheckinPath = "/billing/meter/checkin-activity-status"
	workBuddyCreditsKind = "monthly"
	workBuddyCheckinKind = "checkin"
)

// workBuddySummaryResponse 只解析外壳里的状态码与套餐等级信息。额度明细不走
// 固定结构体，统一由 collectWorkBuddyPackages 扫描得到，见该文件开头说明。
type workBuddySummaryResponse struct {
	Code json.Number          `json:"code"`
	Msg  string               `json:"msg"`
	Data workBuddySummaryData `json:"data"`
}

type workBuddySummaryData struct {
	SubscriptionPackageCode string `json:"SubscriptionPackageCode"`
	IsPaidUser              bool   `json:"IsPaidUser"`
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

// parseWorkBuddyCredits 解析积分资源响应，把每个有效套餐映射成一个 monthly 窗口
// （桌面端把积分按订阅周期滚动结算，月度窗口语义最贴合）。
// 已用取上游值；缺失或为 0 时用 总量 − 剩余 补齐。
//
// 明细用字段名无关的扫描收集：host 根的老接口与 /v2 网关的新接口外壳可能不同，
// 写死一种结构一旦猜错就要再发一版。
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
	for _, item := range collectWorkBuddyPackages(body) {
		packageTotal := item.Total
		if packageTotal > 0 {
			packageRemain := item.Remain
			used := item.Used
			if used <= 0 {
				used = max(packageTotal-packageRemain, 0)
			}
			remaining := min(max(packageRemain, 0), packageTotal)
			percent := used / packageTotal * 100
			view.Windows = append(view.Windows, QuotaWindow{
				Kind:        workBuddyCreditsKind,
				Label:       workBuddyPackageLabel(item.PackageCode),
				UsedPercent: percent,
				Used:        &used,
				Total:       &packageTotal,
				Remaining:   &remaining,
			})
			continue
		}
		// 个别套餐只下发剩余与已用、不给总量：用两者之和还原周期总量。
		if item.Remain <= 0 && item.Used <= 0 {
			continue
		}
		used := max(item.Used, 0)
		remaining := max(item.Remain, 0)
		packageTotal = used + remaining
		percent := used / packageTotal * 100
		view.Windows = append(view.Windows, QuotaWindow{
			Kind:        workBuddyCreditsKind,
			Label:       workBuddyPackageLabel(item.PackageCode),
			UsedPercent: percent,
			Used:        &used,
			Total:       &packageTotal,
			Remaining:   &remaining,
		})
	}
	if len(view.Windows) == 0 {
		return QuotaView{}, gerror.Newf("WorkBuddy 未返回有效的积分额度窗口，原始响应：%s", quotaRawSnippet(body))
	}
	view.Level = workBuddyLevel(payload.Data.SubscriptionPackageCode, payload.Data.IsPaidUser)
	return view, nil
}

// parseWorkBuddyCheckin 解析 checkin-activity-status，生成一条计数型 checkin 窗口：
// 每个账号贡献「已签 1 / 共 1」，合并后自然得到「今天 N/M 个账号已签到」。
// 连续签到天数与今日签到积分放在窗口提示里，不占用计数语义。
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
	checked := 0.0
	if payload.Data.TodayCheckedIn {
		checked = 1
	}
	one := 1.0
	percent := checked * 100
	view := QuotaView{
		Mode:      channeltype.AdapterWorkBuddy,
		QueriedAt: time.Now(),
		Windows: []QuotaWindow{
			{
				Kind:         workBuddyCheckinKind,
				Label:        "每日签到",
				UsedPercent:  percent,
				Counts:       true,
				DoneCount:    &checked,
				AccountCount: &one,
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

// fetchWorkBuddyCreditsForCredential 查询单个上游密钥（一个账号）的积分与签到，
// 供管理端「按账号查积分」使用。与渠道级查询共用同一套积分/签到实现，只是
// 不并发、不合并，失败直接返回，便于逐账号诊断。
func (s *sChannel) fetchWorkBuddyCreditsForCredential(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig, credential quotaCredential) (QuotaView, error) {
	creditsEndpoint, err := resolveHostURL(channel.BaseUrl, workBuddyCreditsPath)
	if err != nil {
		return QuotaView{}, err
	}
	checkinEndpoint, err := resolveHostURL(channel.BaseUrl, "/v2"+workBuddyCheckinPath)
	if err != nil {
		return QuotaView{}, err
	}
	return s.fetchWorkBuddyCreditsSingle(ctx, channel, config, credential, creditsEndpoint, checkinEndpoint)
}

// fetchWorkBuddyCreditsSingle 先查积分，成功后再尽力查签到；签到失败写入
// view.PartialErrors，不向上抛错（避免把一次风控头缺失的降级也当成积分失败）。
func (s *sChannel) fetchWorkBuddyCreditsSingle(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig, credential quotaCredential, creditsEndpoint, checkinEndpoint string) (QuotaView, error) {
	creditsBody, err := s.fetchWorkBuddyCreditsBody(ctx, channel, credential.Cipher, workBuddyCreditsRequest{Base: upstreamJSONRequest{
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
			if status == 401 || status == 403 {
				return gerror.New(workBuddyCreditsRejectedText(status))
			}
			return gerror.Newf("WorkBuddy 积分接口返回 HTTP %d", status)
		},
	}})
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
			// 2026-10-27 用户要求：使用时发现没签到就自动签到。状态查询能读到时
			// 才有依据，读不到（缺 X-Device-Token 风控头）时不盲目领取——那种
			// 情况下无法判断今天是否已签，重复调用没有意义。
			if !workBuddyCheckedInToday(checkinBody) {
				if claim, claimErr := s.claimWorkBuddyCheckin(ctx, channel, config, credential, checkinEndpoint); claimErr != nil {
					view.PartialErrors = append(view.PartialErrors, fmt.Sprintf("密钥 %s：自动签到失败：%s", credential.Prefix, claimErr.Error()))
				} else {
					view.PartialErrors = append(view.PartialErrors, workBuddyCheckinText(credential.Prefix, claim))
				}
			}
		} else {
			view.PartialErrors = append(view.PartialErrors, fmt.Sprintf("密钥 %s：签到状态解析失败：%s", credential.Prefix, parseErr.Error()))
		}
	} else {
		view.PartialErrors = append(view.PartialErrors, fmt.Sprintf("密钥 %s：签到状态查询失败（需 X-Device-Token 风控头）：%s", credential.Prefix, checkinErr.Error()))
	}
	return view, nil
}

// claimWorkBuddyCheckin 调用签到领取接口。签到领取是同族下的
// …/v2/billing/meter/daily-checkin，与状态查询只在最后一段不同。
//
// 这里不能对传入的 checkinEndpoint 做 TrimSuffix：那是 resolveHostURL 拼好的完整
// URL（形如 https://host/v2/billing/meter/checkin-activity-status），而
// workBuddyCheckinPath 不含 /v2 前缀，TrimSuffix 匹配不上，端点会原样保留成
// 状态查询地址，领取请求必然 404（2026-10-27 生产日志实测）。改为直接按渠道
// baseUrl 重新拼领取地址，不依赖入参的字符串形态。
// 注意 workBuddyDailyCheckinPath 常量自带 /v2 前缀，这里不能再补。
func (s *sChannel) claimWorkBuddyCheckin(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig, credential quotaCredential, _ string) (workBuddyCheckinClaim, error) {
	endpoint, err := resolveHostURL(channel.BaseUrl, workBuddyDailyCheckinPath)
	if err != nil {
		return workBuddyCheckinClaim{}, fmt.Errorf("渠道 %d 地址无效：%w", channel.Id, err)
	}
	body, err := s.fetchUpstreamJSON(ctx, channel, credential.Cipher, upstreamJSONRequest{
		Method:       "POST",
		Endpoint:     endpoint,
		AuthType:     config.AuthType,
		HeaderName:   config.HeaderName,
		HeaderPrefix: "Bearer ",
		BodyLimit:    1 << 20,
		RequestError: "创建 WorkBuddy 签到请求失败",
		FetchError:   "请求 WorkBuddy 签到接口失败",
		ReadError:    "读取 WorkBuddy 签到接口响应失败",
		InvalidError: "WorkBuddy 签到接口返回了无效 JSON",
		StatusError: func(status int, payload []byte) error {
			return gerror.Newf("WorkBuddy 签到接口返回 HTTP %d", status)
		},
	})
	if err != nil {
		return workBuddyCheckinClaim{}, err
	}
	return parseWorkBuddyCheckinClaim(body)
}
