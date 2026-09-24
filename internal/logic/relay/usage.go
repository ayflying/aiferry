package relay

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/usage"
)

var ErrUpstreamUsageNotBillable = gerror.New("上游响应未返回可计费的用量信息")

func (s *sRelay) record(ctx context.Context, requestID string, key apikey.AuthKey, candidate Candidate, clientIP, endpoint, requestedModel string, stream bool, attempts int, startedAt time.Time, result attemptResult) error {
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
	var chargeErr error
	if billable {
		if cost == nil {
			chargeErr = ErrUpstreamUsageNotBillable
		} else if chargeable {
			if s.channels != nil {
				if err := s.channels.ApplyCredentialUsageCost(ctx, candidate.ChannelID, candidate.ChannelCredentialID, *cost); err != nil {
					g.Log().Warningf(ctx, "apply channel %d usage cost: %v", candidate.ChannelID, err)
				}
			}
			// 自有渠道（创建者=当前密钥用户）：usage_logs 仍记 estimated_cost 供统计，
			// 但不扣用户余额、不累计密钥消耗，billingDetails.Charged 保持 false。
			ownChannel := candidate.CreatedByUserID != 0 && candidate.CreatedByUserID == key.UserId
			if ownChannel {
				g.Log().Debugf(ctx, "own channel %d usage %s: stats-only, cost=%s", candidate.ChannelID, requestID, cost)
			} else if err := s.users.Debit(ctx, key.UserId, *cost); err != nil {
				chargeErr = err
			} else {
				billingDetails.Charged = true
				_ = apikey.New(s.app).AddSpend(ctx, key, cost.InexactFloat64())
				if s.mail != nil {
					s.mail.NotifyLowBalance(ctx, key.UserId)
				}
			}
		}
		if chargeErr != nil {
			recordStatus = 402
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
		IPLocation:           s.location(clientIP),
		RequestedModel:       requestedModel,
		UpstreamModel:        candidate.UpstreamName,
		ReasoningEffort:      candidate.ReasoningEffort,
		HealthScoreAtRequest: s.modelHealthScoreAtRequest(ctx, candidate),
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
		g.Log().Errorf(ctx, "record usage %s: %v", requestID, err)
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
