package relay

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/service/apikey"
	"github.com/yunloli/aiferry/internal/service/usage"
)

func (s *Service) record(ctx context.Context, requestID string, key apikey.AuthKey, candidate Candidate, endpoint, requestedModel string, stream bool, attempts int, startedAt time.Time, result attemptResult) error {
	upstreamEndpoint := result.upstreamEndpoint
	if upstreamEndpoint == "" {
		upstreamEndpoint = endpoint
	}
	cost := s.prices.Estimate(candidate.PublicName, upstreamEndpoint, result.tokens)
	recordStatus := result.status
	recordError := result.errorMessage
	var chargeErr error
	if result.status >= 200 && result.status < 300 {
		if cost == nil {
			chargeErr = gerror.New("上游响应未返回可计费的用量信息")
		} else {
			if s.channels != nil {
				if err := s.channels.ApplyCredentialUsageCost(ctx, candidate.ChannelID, candidate.ChannelCredentialID, *cost); err != nil {
					g.Log().Warningf(ctx, "apply channel %d usage cost: %v", candidate.ChannelID, err)
				}
			}
			if err := s.users.Debit(ctx, key.UserId, *cost); err != nil {
				chargeErr = err
			} else {
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
	if err := s.usage.Record(ctx, usage.RecordInput{
		RequestID:           requestID,
		UserID:              key.UserId,
		APIKeyID:            key.Id,
		ChannelID:           candidate.ChannelID,
		ChannelCredentialID: candidate.ChannelCredentialID,
		Endpoint:            endpoint,
		UpstreamEndpoint:    upstreamEndpoint,
		ProtocolConversion:  result.protocolConversion,
		RequestedModel:      requestedModel,
		UpstreamModel:       candidate.UpstreamName,
		HTTPStatus:          recordStatus,
		Stream:              stream,
		Tokens:              result.tokens,
		EstimatedCost:       cost,
		DurationMs:          time.Since(startedAt).Milliseconds(),
		FirstTokenMs:        result.firstTokenMs,
		Attempts:            attempts,
		ErrorMessage:        recordError,
	}); err != nil {
		g.Log().Errorf(ctx, "record usage %s: %v", requestID, err)
	}
	return chargeErr
}

func parseJSONUsage(body []byte) usage.TokenUsage {
	return usage.ParseJSONUsage(body)
}

func parseSSEUsage(line []byte, target *usage.TokenUsage) {
	usage.ParseSSEUsage(line, target)
}

func ruleCost(conditionsJSON, ratesJSON, endpoint string, tokens usage.TokenUsage) (*decimal.Decimal, bool) {
	return usage.RuleCost(conditionsJSON, ratesJSON, endpoint, tokens)
}
