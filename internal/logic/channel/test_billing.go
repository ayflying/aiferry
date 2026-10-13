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
