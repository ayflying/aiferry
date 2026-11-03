package channel

import (
	"context"
	"net/http"
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
	billingDetails := s.prices.EstimateBreakdown(model.PublicName, path, tokens, time.Now())
	var cost *decimal.Decimal
	if billingDetails != nil {
		amount := billingDetails.Cost()
		cost = &amount
	}
	recordStatus := result.HTTPStatus
	recordMessage := result.Message
	// 测试请求失败时同样可能已经消耗上游 token：保留计费快照并写标记价，
	// 让管理端的测试记录也显示真实成本，而不是「未定价」。
	if !result.Success {
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

// failureTestBilling 把失败测试请求的成本口径改成「已计费但不扣款」：
// 与转发侧 failedUsageBilling 一致——库里写标记价，真实金额只留在快照里。
func failureTestBilling(billingDetails *usage.BillingBreakdown) (*decimal.Decimal, *usage.BillingBreakdown) {
	if billingDetails == nil {
		return nil, nil
	}
	details := usage.NewFailedBillingDetails(&usage.FailedBillingDetails{
		Currency: billingDetails.Currency,
		Items:    billingDetails.Items,
		Subtotal: billingDetails.Subtotal,
		Total:    billingDetails.Total,
	})
	if details == nil {
		return nil, nil
	}
	marked := usage.FailureCost()
	return &marked, details
}
