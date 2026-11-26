package channel

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// testChargesUser 与正式转发一致：自有渠道只统计上游成本，不扣账户余额。
// 系统巡检继续免扣费；创建者未知时不得误判为自有渠道。
func testChargesUser(userID, creatorID uint64) bool {
	return userID != usage.SystemUserID && !(creatorID != 0 && creatorID == userID)
}

func (s *sChannel) recordTestUsage(ctx context.Context, userID uint64, channel entity.Channels, credentialID uint64, model entity.ChannelModels, path string, result *TestResult, tokens usage.TokenUsage, attemptFlow []usage.AttemptFlowStep) error {
	if s.usage == nil {
		return nil
	}
	billingDetails := s.testBillingBreakdown(model, path, tokens, time.Now(), false)
	var cost *decimal.Decimal
	if billingDetails != nil {
		amount := billingDetails.Cost()
		cost = &amount
	}
	recordStatus := result.HTTPStatus
	recordMessage := result.Message
	// 失败测试保留套餐和费率，包括无 usage 的零数量项，但不产生费用。
	if !result.Success {
		billingDetails = s.testBillingBreakdown(model, path, tokens, time.Now(), true)
		cost, billingDetails = failureTestBilling(billingDetails)
	}
	var chargeErr error
	if result.Success {
		if cost != nil {
			if applyErr := s.ApplyCredentialUsageCost(ctx, channel.Id, credentialID, *cost); applyErr != nil {
				g.Log().Warningf(ctx, "apply channel %d test usage cost: %v", channel.Id, applyErr)
			}
			if testChargesUser(userID, channel.CreatedByUserId) {
				if debitErr := s.users.Debit(ctx, userID, *cost); debitErr != nil {
					chargeErr = debitErr
				} else {
					billingDetails.Charged = true
					if s.mail != nil {
						s.mail.NotifyLowBalance(ctx, userID)
					}
				}
			}
		}
		if chargeErr != nil {
			recordStatus = http.StatusPaymentRequired
			recordMessage = chargeErr.Error()
		}
	}
	recordErr := s.usage.Record(ctx, usage.RecordInput{
		RequestID:            usage.NewRequestID("aftest"),
		UserID:               userID,
		ChannelID:            channel.Id,
		ChannelCredentialID:  credentialID,
		Endpoint:             "test:" + path,
		RequestedModel:       model.PublicName,
		UpstreamModel:        model.UpstreamName,
		HealthScoreAtRequest: &model.HealthScore,
		HTTPStatus:           recordStatus,
		Stream:               result.Stream,
		Tokens:               tokens,
		EstimatedCost:        cost,
		BillingDetails:       billingDetails,
		DurationMs:           result.LatencyMs,
		Attempts:             len(attemptFlow),
		AttemptFlow:          attemptFlow,
		ErrorMessage:         recordMessage,
	})
	if recordErr != nil {
		result.Message = truncate(result.Message+"；用量记录失败："+recordErr.Error(), 1024)
	}
	return chargeErr
}

// modelPriceNames 与转发链路同一口径：先按对外公开名取价，公开名还没配价格时
// 回退上游模型名。测试扣费必须与正式计费一致，否则会出现「测试显示免费、
// 正式请求却收费」这种对不上的情况。
func modelPriceNames(model entity.ChannelModels) []string {
	names := make([]string, 0, 2)
	if name := strings.TrimSpace(model.PublicName); name != "" {
		names = append(names, name)
	}
	if name := strings.TrimSpace(model.UpstreamName); name != "" && name != model.PublicName {
		names = append(names, name)
	}
	return names
}

func (s *sChannel) modelIsPriced(model entity.ChannelModels) bool {
	for _, name := range modelPriceNames(model) {
		if s.prices.IsPriced(name) {
			return true
		}
	}
	return false
}

// testBillingBreakdown 按 modelPriceNames 的顺序取第一个可算出的账单，
// 并把实际取价的模型写进快照，测试账单与正式账单因此能对得上。
func (s *sChannel) testBillingBreakdown(model entity.ChannelModels, path string, tokens usage.TokenUsage, at time.Time, failure bool) *usage.BillingBreakdown {
	for _, name := range modelPriceNames(model) {
		var breakdown *usage.BillingBreakdown
		if failure {
			breakdown = s.prices.EstimateFailureBreakdown(name, path, tokens, at)
		} else {
			breakdown = s.prices.EstimateBreakdown(name, path, tokens, at)
		}
		if breakdown != nil {
			breakdown.PricedModel = name
			return breakdown
		}
	}
	return nil
}

// failureTestBilling 与转发侧保持一致：保留价格元数据，费用归零且不扣款。
func failureTestBilling(billingDetails *usage.BillingBreakdown) (*decimal.Decimal, *usage.BillingBreakdown) {
	cost := usage.FailureCost()
	if billingDetails == nil {
		return &cost, nil
	}
	details := usage.NewFailedBillingDetails(&usage.FailedBillingDetails{
		BillingMode: billingDetails.BillingMode,
		Currency:    billingDetails.Currency,
		Rule:        billingDetails.Rule,
		Items:       billingDetails.Items,
	})
	return &cost, details
}
