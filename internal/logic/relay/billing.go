package relay

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// 本文件实现 OpenAI 兼容的账单查询 API：
//   - GET /v1/dashboard/billing/subscription —— 密钥所属用户的余额与该密钥的限额视图
//   - GET /v1/dashboard/billing/usage?start_date=&end_date= —— 该密钥在时间窗口内的用量
//   - GET /v1/dashboard/billing/channels —— 仅管理员角色密钥：各渠道最新余额快照
//
// 返回结构对齐 OpenAI legacy dashboard API（one-api 等网关的同名接口），方便
// 已有生态工具（余额监控脚本、ChatGPT-Next-Web 等）直接接入。

// SubscriptionView 对齐 OpenAI GET /v1/dashboard/billing/subscription 响应。
// hard_limit_usd 语义为本网关的「可用额度」：用户总余额与密钥剩余限额取小值。
type SubscriptionView struct {
	HardLimitUSD           float64  `json:"hard_limit_usd"`
	SystemHardLimitGranted float64  `json:"system_hard_limit_granted"`
	AccessUntil            int64    `json:"access_until"`
	HasPaymentMethod       bool     `json:"has_payment_method"`
	Plan                   PlanView `json:"plan"`
}

type PlanView struct {
	Title  string `json:"title"`
	IsPaid bool   `json:"is_paid"`
}

// UsageDaily 一次日维度用量条目。
type UsageDaily struct {
	Timestamp           float64 `json:"timestamp"`
	NTokens1k           float64 `json:"n_tokens_1k,omitempty"`
	NRequests           int64   `json:"n_requests"`
	NumModelInvocations int64   `json:"num_model_invocations"`
}

// UsageView 对齐 OpenAI GET /v1/dashboard/billing/usage 响应；
// total_usage 为窗口内估算费用合计（美元口径），其余为本网关补充字段。
type UsageView struct {
	Object        string       `json:"object"`
	TotalUsage    float64      `json:"total_usage"`
	DailyCosts    []UsageDaily `json:"daily_costs"`
	TotalLines    int          `json:"total_lines"`
	TotalTokens   int64        `json:"total_tokens"`
	TotalRequests int64        `json:"total_requests"`
}

// ChannelBalanceView 管理员视角的单渠道余额快照（来自成本同步任务
// 写入 channels 表的 LastCost* 字段，不触发上游实时查询）。
type ChannelBalanceView struct {
	ChannelID uint64     `json:"channelId"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	Status    int        `json:"status"`
	Used      *float64   `json:"used,omitempty"`
	Remaining *float64   `json:"remaining,omitempty"`
	Currency  string     `json:"currency,omitempty"`
	QueriedAt *time.Time `json:"queriedAt,omitempty"`
}

// usageMaxWindow 用量查询的最大时间窗口。
const usageMaxWindow = 100 * 24 * time.Hour

// channelsBalanceLimit 单次返回的渠道数上限。
const channelsBalanceLimit = 200

// BillingError 种类标记：权限不足（403）与请求非法（400）分别映射 HTTP 状态。
type billingErrorKind int

const (
	billingErrorInternal billingErrorKind = iota
	billingErrorPermission
	billingErrorRequest
)

type billingError struct {
	kind billingErrorKind
	err  error
}

func (e billingError) Error() string { return e.err.Error() }
func (e billingError) Unwrap() error { return e.err }

// IsBillingPermissionError 判断是否为密钥无权访问该账单端点。
func IsBillingPermissionError(err error) bool {
	var target billingError
	return errorsAsBilling(err, &target) && target.kind == billingErrorPermission
}

// IsBillingRequestError 判断是否为客户端请求参数错误。
func IsBillingRequestError(err error) bool {
	var target billingError
	return errorsAsBilling(err, &target) && target.kind == billingErrorRequest
}

func errorsAsBilling(err error, target *billingError) bool {
	return errors.As(err, target)
}

func billingPermissionError(message string) error {
	return billingError{kind: billingErrorPermission, err: gerror.New(message)}
}

func billingRequestError(message string) error {
	return billingError{kind: billingErrorRequest, err: gerror.New(message)}
}

// Subscription 返回 OpenAI 兼容的密钥余额视图。
func (s *sRelay) Subscription(ctx context.Context, key apikey.AuthKey) (SubscriptionView, error) {
	view, err := s.subscriptionFor(ctx, key)
	if err != nil {
		return SubscriptionView{}, err
	}
	return view, nil
}

// Usage 返回该密钥在时间窗口内的用量（OpenAI 兼容格式）。
func (s *sRelay) Usage(ctx context.Context, key apikey.AuthKey, start, end time.Time) (UsageView, error) {
	if !start.Before(end) {
		return UsageView{}, billingRequestError("start_date must be before end_date")
	}
	if end.Sub(start) > usageMaxWindow {
		return UsageView{}, billingRequestError("date range must not exceed 100 days")
	}
	return s.usageFor(ctx, key, start, end)
}

// ChannelsBalance 返回各渠道最新余额快照，仅管理员角色密钥可用。
func (s *sRelay) ChannelsBalance(ctx context.Context, key apikey.AuthKey) ([]ChannelBalanceView, error) {
	views, err := s.channelsBalanceFor(ctx, key)
	if err != nil {
		return nil, err
	}
	return views, nil
}

func sanitizeAmount(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

// subscriptionFor 用 Bearer 密钥查询所属用户余额与密钥限额视图。
func (s *sRelay) subscriptionFor(ctx context.Context, key apikey.AuthKey) (SubscriptionView, error) {
	var profile entity.Users
	if err := dao.Users.Ctx(ctx).Where(dao.Users.Columns().Id, key.UserId).Scan(&profile); err != nil {
		return SubscriptionView{}, gerror.Wrap(err, "load user balance")
	}
	if profile.Id == 0 {
		return SubscriptionView{}, billingRequestError("user not found")
	}

	// hard_limit_usd = 用户总余额；密钥设置了 spend limit 时取两者剩余的
	// 较小值，与扣费侧的实际约束一致（密钥超限会被拒绝）。
	limit := sanitizeAmount(profile.Balance)
	if key.SpendLimit != nil {
		keyRemaining := *key.SpendLimit - key.SpentAmount
		if keyRemaining < limit {
			limit = keyRemaining
		}
		if limit < 0 {
			limit = 0
		}
	}
	return SubscriptionView{
		HardLimitUSD:           limit,
		SystemHardLimitGranted: 0,
		AccessUntil:            time.Now().AddDate(1, 0, 0).Unix(),
		HasPaymentMethod:       true,
		Plan: PlanView{
			Title:  "Pay as you go",
			IsPaid: true,
		},
	}, nil
}

// usageFor 统计该密钥在 [start, end) 窗口内的用量（按日聚合）。
// 数据来源 usage_logs（与控制台用量页同一事实来源）。
func (s *sRelay) usageFor(ctx context.Context, key apikey.AuthKey, start, end time.Time) (UsageView, error) {
	if !start.Before(end) {
		return UsageView{}, gerror.New("start_date must be before end_date")
	}
	if end.Sub(start) > 100*24*time.Hour {
		return UsageView{}, gerror.New("date range must not exceed 100 days")
	}

	columns := dao.UsageLogs.Columns()
	type usageRow struct {
		Day      string  `orm:"day"`
		Cost     float64 `orm:"cost"`
		Tokens   int64   `orm:"tokens"`
		Requests int64   `orm:"requests"`
	}
	rows := make([]usageRow, 0)
	err := dao.UsageLogs.Ctx(ctx).
		Fields(
			"DATE_FORMAT("+columns.CreatedAt+", '%Y-%m-%d') AS day",
			"COALESCE(SUM("+columns.EstimatedCost+"), 0) AS cost",
			"COALESCE(SUM("+columns.TotalTokens+"), 0) AS tokens",
			"COUNT(*) AS requests",
		).
		Where(columns.ApiKeyId, key.Id).
		WhereGTE(columns.CreatedAt, start).
		WhereLT(columns.CreatedAt, end).
		Group("day").
		OrderAsc("day").
		Scan(&rows)
	if err != nil {
		return UsageView{}, gerror.Wrap(err, "aggregate key usage")
	}

	daily := make([]UsageDaily, 0, len(rows))
	var totalUsage float64
	var totalTokens int64
	var totalRequests int64
	for _, row := range rows {
		ts, parseErr := time.ParseInLocation("2006-01-02", row.Day, time.Local)
		if parseErr != nil {
			continue
		}
		daily = append(daily, UsageDaily{
			Timestamp:           float64(ts.Unix()),
			NTokens1k:           float64(row.Tokens) / 1000,
			NRequests:           row.Requests,
			NumModelInvocations: row.Requests,
		})
		totalUsage += row.Cost
		totalTokens += row.Tokens
		totalRequests += row.Requests
	}
	return UsageView{
		Object:        "list",
		TotalUsage:    totalUsage,
		DailyCosts:    daily,
		TotalLines:    len(daily),
		TotalTokens:   totalTokens,
		TotalRequests: totalRequests,
	}, nil
}

// channelsBalanceFor 管理员密钥查询全部渠道的最新余额快照。
func (s *sRelay) channelsBalanceFor(ctx context.Context, key apikey.AuthKey) ([]ChannelBalanceView, error) {
	if !s.app.Config.IsAdminRole(key.UserRole) {
		return nil, billingPermissionError("该访问密钥所属用户不是管理员，无法查询渠道余额")
	}
	columns := dao.Channels.Columns()
	rows := make([]entity.Channels, 0, 16)
	err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.Name, columns.Type, columns.Status, columns.LastCostUsed, columns.LastCostRemaining, columns.LastCostCurrency, columns.LastCostAt).
		Where(columns.Status, 1).
		OrderAsc(columns.Id).
		Limit(channelsBalanceLimit).
		Scan(&rows)
	if err != nil {
		return nil, gerror.Wrap(err, "list channel balances")
	}
	views := make([]ChannelBalanceView, 0, len(rows))
	for _, row := range rows {
		view := ChannelBalanceView{
			ChannelID: row.Id,
			Name:      row.Name,
			Type:      row.Type,
			Status:    row.Status,
		}
		if row.LastCostUsed != 0 {
			used := row.LastCostUsed
			view.Used = &used
		}
		if row.LastCostRemaining != 0 {
			remaining := row.LastCostRemaining
			view.Remaining = &remaining
		}
		if row.LastCostCurrency != "" {
			view.Currency = row.LastCostCurrency
		}
		if !row.LastCostAt.IsZero() {
			at := row.LastCostAt
			view.QueriedAt = &at
		}
		views = append(views, view)
	}
	return views, nil
}
