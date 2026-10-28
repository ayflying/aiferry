package channel

import (
	"context"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/schedule"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// WorkBuddy 每日自动签到后台任务。
//
// 触发点有两个，互为补充：
//  1. 额度查询时（quota_workbuddy.go）：用户打开渠道额度详情、或页面刷新时
//     顺手签到，属于「使用的时候发现没签到就签」；
//  2. 本文件的后台任务：即使用户当天完全没打开管理端，也会在每天固定时刻
//     遍历所有 WorkBuddy 渠道下的每个账号各签一次。
//
// 两个入口都只对「读到明确未签到」的账号调用领取接口，并且签到本身在上游是
// 幂等的（重复调用返回 code=0、credit=0），因此同一天重复触发不会出错。
const (
	// workBuddyCheckinHour / workBuddyCheckinMinute 是每天执行签到的时刻
	// （服务端本地时区）。签到按自然日生效，每天一次就够；定在早上 6 点是为了
	// 避开跨零点时的上游日切边界，也避开凌晨可能的上游维护窗口。
	//
	// 2026-10-28 用户指出：签到是每日一次的动作，不该按小时反复检查。
	// 之前用 KindHourlyAfter 每小时检查一次，虽然已签到的账号会跳过、不会重复
	// 领取，但每小时都打一轮上游状态请求既没有意义，也让日志噪声变大。
	workBuddyCheckinHour   = 6
	workBuddyCheckinMinute = 0
	// workBuddyCheckinStartupDelay 是启动后首轮补签的等待时间：够启动流程跑完
	// 渠道同步，又不至于让发版当天的补签拖太久。
	//
	// 这一轮是「补签」而不是「多签」：它先读当天签到状态，已签到的账号直接跳过。
	// 保留它是为了让发版/重启当天不漏签——若只等下一个 6 点，当天可能整天没签。
	workBuddyCheckinStartupDelay = 90 * time.Second
)

// workBuddyCheckinTaskKey 是「WorkBuddy 每日签到」在计划任务中心的标识。
const workBuddyCheckinTaskKey = "channel.workbuddy_checkin"

// StartWorkBuddyCheckins 启动每日自动签到任务。与健康检查一致，由 app 层在
// 服务启动时调用一次；ctx 结束时协程自行退出。
//
// 启动后先等一个短延迟跑首轮：一是避开启动初期的模型/渠道同步风暴，二是让
// 服务重启或发版当天就能补签，不必等到下一个整点。之后按小时粒度检查。
func (s *sChannel) StartWorkBuddyCheckins(ctx context.Context) {
	schedule.Register(schedule.Spec{
		Key:         workBuddyCheckinTaskKey,
		Name:        "WorkBuddy 每日签到",
		Description: "每天 6:00 执行一次；遍历所有 WorkBuddy 渠道的每个账号，未签到的自动领取签到奖励。服务启动 90 秒后若已过 6 点先补跑一轮（已签到的账号自动跳过）。",
		Kind:        schedule.KindDailyAt,
		Hour:        workBuddyCheckinHour,
		Minute:      workBuddyCheckinMinute,
		Enabled:     true,
	})
	go func() {
		// 启动补签：发版或重启当天已过 6 点时立刻补一轮，避免等满一天。
		firstRun := time.NewTimer(workBuddyCheckinStartupDelay)
		defer firstRun.Stop()
		select {
		case <-ctx.Done():
			return
		case <-firstRun.C:
			if time.Now().Hour() >= workBuddyCheckinHour {
				s.runWorkBuddyCheckinsOnce(ctx)
			}
		}
		// 之后每天只在固定时刻跑一次。用「算到下一个 6:00 再睡」而不是按小时
		// 轮询：签到是自然日一次的动作，每小时都问一遍上游既无意义又刷日志。
		for {
			wait := time.Until(nextWorkBuddyCheckinAt(time.Now()))
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.runWorkBuddyCheckinsOnce(ctx)
			}
		}
	}()
}

// nextWorkBuddyCheckinAt 返回下一次签到的时刻：今天的 6:00，已经过了就是明天。
// 抽成独立函数便于单测覆盖跨日/整点边界。
func nextWorkBuddyCheckinAt(from time.Time) time.Time {
	next := time.Date(from.Year(), from.Month(), from.Day(), workBuddyCheckinHour, workBuddyCheckinMinute, 0, 0, from.Location())
	if !next.After(from) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// runWorkBuddyCheckinsOnce 在一轮签到外加上计划任务的状态上报，让管理端的
// 任务中心能看到执行次数、耗时与失败原因。
func (s *sChannel) runWorkBuddyCheckinsOnce(ctx context.Context) {
	_ = schedule.Run(ctx, workBuddyCheckinTaskKey, func(ctx context.Context) error {
		s.runWorkBuddyCheckins(ctx)
		return nil
	})
}

// runWorkBuddyCheckins 遍历所有启用中的 WorkBuddy 渠道，对每个渠道下的每个账号
// 兜底签一次。单个渠道/账号失败只记日志，不影响其它渠道。
func (s *sChannel) runWorkBuddyCheckins(ctx context.Context) {
	var channels []entity.Channels
	if err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Type, "workbuddy").Scan(&channels); err != nil {
		g.Log().Warningf(ctx, "workbuddy 自动签到：读取渠道失败：%v", err)
		return
	}
	g.Log().Infof(ctx, "workbuddy 自动签到：本轮共 %d 个 WorkBuddy 渠道待检查", len(channels))
	for _, channel := range channels {
		s.checkinWorkBuddyChannel(ctx, channel)
	}
}

// checkinWorkBuddyChannel 对单个渠道下的全部账号并发签到，并汇总结果到日志。
// 与额度查询共用 claimWorkBuddyCheckin，保证「查询时签到」和「后台签到」走的
// 是同一套请求与解析逻辑，不存在两处实现漂移。
func (s *sChannel) checkinWorkBuddyChannel(ctx context.Context, channel entity.Channels) {
	credentials, err := s.credentialCiphers(ctx, channel.Id)
	if err != nil {
		g.Log().Warningf(ctx, "workbuddy 自动签到：渠道 %d 读取密钥失败：%v", channel.Id, err)
		return
	}
	checkinEndpoint, err := resolveHostURL(channel.BaseUrl, "/v2"+workBuddyCheckinPath)
	if err != nil {
		g.Log().Warningf(ctx, "workbuddy 自动签到：渠道 %d 地址无效：%v", channel.Id, err)
		return
	}
	config, ok := s.workBuddyQuotaConfig(ctx, channel)
	if !ok {
		g.Log().Warningf(ctx, "workbuddy 自动签到：渠道 %d 未声明 workbuddy_credits 额度适配器，跳过", channel.Id)
		return
	}
	var wg sync.WaitGroup
	for _, credential := range credentials {
		wg.Add(1)
		go func(credential quotaCredential) {
			defer wg.Done()
			// 先读状态：明确已签到就跳过，避免无意义的上游调用。
			statusBody, statusErr := s.fetchWorkBuddyCheckinStatus(ctx, channel, config, credential, checkinEndpoint)
			if statusErr == nil && workBuddyCheckedInToday(statusBody) {
				g.Log().Infof(ctx, "workbuddy 自动签到：渠道 %d 密钥 %s 今天已签到，跳过", channel.Id, credential.Prefix)
				return
			}
			if statusErr != nil {
				g.Log().Infof(ctx, "workbuddy 自动签到：渠道 %d 密钥 %s 状态未知（%v），直接尝试领取", channel.Id, credential.Prefix, statusErr)
			}
			claim, claimErr := s.claimWorkBuddyCheckin(ctx, channel, config, credential, checkinEndpoint)
			if claimErr != nil {
				g.Log().Warningf(ctx, "workbuddy 自动签到：渠道 %d 密钥 %s 失败：%v", channel.Id, credential.Prefix, claimErr)
				return
			}
			g.Log().Infof(ctx, "workbuddy 自动签到：渠道 %d 密钥 %s %s", channel.Id, credential.Prefix, workBuddyCheckinText(credential.Prefix, claim))
		}(credential)
	}
	wg.Wait()
}

// workBuddyQuotaConfig 解析渠道声明的额度配置；非 workbuddy 积分适配器直接跳过。
// 与 QueryQuota 用的是同一个渠道类型配置来源，不另建一套解析。
func (s *sChannel) workBuddyQuotaConfig(ctx context.Context, channel entity.Channels) (channeltype.QuotaConfig, bool) {
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		g.Log().Warningf(ctx, "workbuddy 自动签到：渠道 %d 读取类型配置失败：%v", channel.Id, err)
		return channeltype.QuotaConfig{}, false
	}
	if config.Quota.Adapter != channeltype.AdapterWorkBuddy {
		return channeltype.QuotaConfig{}, false
	}
	return config.Quota, true
}

// fetchWorkBuddyCheckinStatus 读取签到状态。单独抽出来是为了让后台任务能在
// 「已签到」时提前返回，不与额度查询的错误降级逻辑混在一起。
func (s *sChannel) fetchWorkBuddyCheckinStatus(ctx context.Context, channel entity.Channels, config channeltype.QuotaConfig, credential quotaCredential, checkinEndpoint string) ([]byte, error) {
	return s.fetchUpstreamJSON(ctx, channel, credential.Cipher, upstreamJSONRequest{
		Method:       "POST",
		Endpoint:     checkinEndpoint,
		AuthType:     config.AuthType,
		HeaderName:   config.HeaderName,
		HeaderPrefix: "Bearer ",
		BodyLimit:    1 << 20,
		RequestError: "创建 WorkBuddy 签到状态查询请求失败",
		FetchError:   "请求 WorkBuddy 签到状态接口失败",
		ReadError:    "读取 WorkBuddy 签到状态接口响应失败",
		InvalidError: "WorkBuddy 签到状态接口返回了无效 JSON",
		StatusError: func(int, []byte) error {
			return nil
		},
	})
}
