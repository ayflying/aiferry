package relay

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/cluster"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/logic/user"
)

var ErrUpstreamUsageNotBillable = gerror.New("上游响应未返回可计费的用量信息")

func (s *sRelay) record(ctx context.Context, requestID string, key apikey.AuthKey, candidate Candidate, clientIP, endpoint, requestedModel string, stream bool, attempts int, startedAt time.Time, result attemptResult) error {
	return cluster.WithWorkerLedger(ctx, func(ledgerCtx context.Context) error {
		return s.recordUsage(ledgerCtx, requestID, key, candidate, clientIP, endpoint, requestedModel, stream, attempts, startedAt, result)
	})
}

func (s *sRelay) recordUsage(ctx context.Context, requestID string, key apikey.AuthKey, candidate Candidate, clientIP, endpoint, requestedModel string, stream bool, attempts int, startedAt time.Time, result attemptResult) error {
	upstreamEndpoint := result.upstreamEndpoint
	if upstreamEndpoint == "" {
		upstreamEndpoint = endpoint
	}
	billingDetails := s.prices.EstimateBreakdown(candidate.PublicName, upstreamEndpoint, result.tokens, startedAt)
	cost, chargeable := pricedUsageCost(s.requiresBalanceCheck(candidate.PublicName), billingDetails)
	recordStatus := result.status
	recordError := result.errorMessage
	// 流式响应被截断时上游可能已经报了 200，但客户端拿到的不是完整回答：
	// 按上游故障记录，并且不扣费。
	billable := result.status >= http.StatusOK && result.status < http.StatusMultipleChoices
	if billable && streamTruncated(stream, result) {
		recordStatus = http.StatusBadGateway
		billable = false
		if strings.TrimSpace(recordError) == "" {
			recordError = streamTruncatedReason
		}
	}
	// 只在需要扣款时开启跨表事务；纯日志路径仅有单条 INSERT，本身已原子。
	// 元数据查询留在事务外，避免 SQLite 延迟事务先读后写升级锁。
	ownChannel := candidate.CreatedByUserID != 0 && candidate.CreatedByUserID == key.UserId
	location := s.location(clientIP)
	healthScore := s.modelHealthScoreAtRequest(ctx, candidate)
	var chargeErr error
	charged := false
	persist := func(ctx context.Context) error {
		if billable {
			if cost == nil {
				chargeErr = ErrUpstreamUsageNotBillable
			} else if chargeable && !ownChannel {
				if err := s.users.Debit(ctx, key.UserId, *cost); err != nil {
					if !user.IsInsufficientBalance(err) {
						return gerror.Wrap(err, "debit relay usage")
					}
					// 余额不足是业务拒绝，仍提交对应的 402 用量日志。
					chargeErr = err
				} else {
					if err := apikey.New(s.app).AddSpend(ctx, key, cost.InexactFloat64()); err != nil {
						return gerror.Wrap(err, "add relay API key spend")
					}
					billingDetails.Charged = true
					charged = true
				}
			}
			if chargeErr != nil {
				recordStatus = http.StatusPaymentRequired
				recordError = chargeErr.Error()
			}
		}
		recordError = detailedFailureLog(result, recordStatus, recordError, stream, attempts, time.Since(startedAt).Milliseconds())
		if err := s.usage.Record(ctx, usage.RecordInput{
			RequestID:            requestID,
			UserID:               key.UserId,
			APIKeyID:             key.Id,
			ChannelID:            candidate.ChannelID,
			ChannelCredentialID:  candidate.ChannelCredentialID,
			Endpoint:             endpoint,
			UpstreamEndpoint:     upstreamEndpoint,
			ProtocolConversion:   result.protocolConversion,
			ClientIP:             clientIP,
			IPLocation:           location,
			RequestedModel:       requestedModel,
			UpstreamModel:        candidate.UpstreamName,
			ReasoningEffort:      candidate.ReasoningEffort,
			HealthScoreAtRequest: healthScore,
			HTTPStatus:           recordStatus,
			Stream:               stream,
			Tokens:               result.tokens,
			EstimatedCost:        cost,
			BillingDetails:       billingDetails,
			DurationMs:           time.Since(startedAt).Milliseconds(),
			FirstTokenMs:         result.firstTokenMs,
			Attempts:             attempts,
			AttemptFlow:          result.attemptFlow,
			ErrorMessage:         recordError,
		}); err != nil {
			return gerror.Wrap(err, "persist relay usage")
		}
		return nil
	}
	var err error
	if billable && chargeable && !ownChannel && cost != nil && cost.IsPositive() {
		err = dao.UsageLogs.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
			return persist(txCtx)
		})
	} else {
		err = persist(ctx)
	}
	if err != nil {
		return err
	}
	// 通知和渠道统计不是核心账本；提交之后才触发，不携带已结束的 txCtx。
	if billable && chargeable && cost != nil && s.channels != nil {
		if err := s.channels.ApplyCredentialUsageCost(ctx, candidate.ChannelID, candidate.ChannelCredentialID, *cost); err != nil {
			g.Log().Warningf(ctx, "apply channel %d usage cost: %v", candidate.ChannelID, err)
		}
	}
	if charged && s.mail != nil {
		s.mail.NotifyLowBalance(ctx, key.UserId)
	}
	return chargeErr
}

func (s *sRelay) missingBillableUsage(candidate Candidate, endpoint string, result attemptResult) bool {
	if result.status < 200 || result.status >= 300 {
		return false
	}
	if !s.requiresBalanceCheck(candidate.PublicName) {
		return false
	}
	upstreamEndpoint := result.upstreamEndpoint
	if upstreamEndpoint == "" {
		upstreamEndpoint = endpoint
	}
	// 这里是对「刚刚拿到的这份响应」做实时判断，用当前时刻折算生效时段即可；
	// 真正落账的金额在 record 里按请求起始时刻重算。
	return s.prices.EstimateBreakdown(candidate.PublicName, upstreamEndpoint, result.tokens, time.Now()) == nil
}

func (s *sRelay) requiresBalanceCheck(modelName string) bool {
	return s.prices.IsPriced(modelName)
}

// candidatesRequireBalanceCheck 只要有一个候选的公开名已定价就需要余额预检：
// 请求可以写映射后的公开名，也可以写上游原始名，实际计费与预检都按
// candidate.PublicName 判定，避免用上游原始名绕过余额预检。
func (s *sRelay) candidatesRequireBalanceCheck(candidates []Candidate) bool {
	for _, candidate := range candidates {
		if s.requiresBalanceCheck(candidate.PublicName) {
			return true
		}
	}
	return false
}

func pricedUsageCost(priced bool, billingDetails *usage.BillingBreakdown) (*decimal.Decimal, bool) {
	if billingDetails != nil {
		cost := billingDetails.Cost()
		return &cost, true
	}
	if !priced {
		freeCost := decimal.Zero
		return &freeCost, false
	}
	return nil, false
}

// modelHealthScoreAtRequest 查询本次请求所用模型记录的当前健康分，作为日志快照。
// 必须在 ApplyModelHealthScore 加减分之前调用（record 先于健康分更新执行），
// 拿到的正是"请求发生时"的分数。查询失败降级为 nil，不阻塞用量记录。
func (s *sRelay) modelHealthScoreAtRequest(ctx context.Context, candidate Candidate) *int {
	if candidate.ChannelModelID == 0 {
		return nil
	}
	columns := dao.ChannelModels.Columns()
	score, err := dao.ChannelModels.Ctx(ctx).
		Fields(columns.HealthScore).
		Where(columns.Id, candidate.ChannelModelID).
		Value()
	if err != nil {
		g.Log().Warningf(ctx, "load health score for usage log model %d: %v", candidate.ChannelModelID, err)
		return nil
	}
	if score == nil || score.IsNil() {
		return nil
	}
	value := score.Int()
	return &value
}

func (s *sRelay) location(clientIP string) string {
	if s.locations == nil {
		return ""
	}
	return s.locations.Lookup(clientIP)
}

func parseJSONUsage(body []byte) usage.TokenUsage {
	return usage.ParseJSONUsage(body)
}

func parseSSEUsage(line []byte, target *usage.TokenUsage) {
	usage.ParseSSEUsage(line, target)
}

func ruleCost(conditionsJSON, ratesJSON, endpoint string, tokens usage.TokenUsage, at time.Time) (*decimal.Decimal, bool) {
	return usage.RuleCost(conditionsJSON, ratesJSON, endpoint, tokens, at)
}
