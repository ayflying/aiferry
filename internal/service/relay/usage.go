package relay

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/service/apikey"
	"github.com/yunloli/aiferry/internal/service/usage"
)

func (s *Service) record(ctx context.Context, requestID string, key apikey.AuthKey, candidate Candidate, endpoint, requestedModel string, stream bool, attempts int, startedAt time.Time, result attemptResult) {
	cost := s.estimateCost(ctx, candidate, endpoint, result.tokens)
	_ = s.usage.Record(ctx, usage.RecordInput{
		RequestID:      requestID,
		UserID:         key.UserId,
		APIKeyID:       key.Id,
		ChannelID:      candidate.ChannelID,
		Endpoint:       endpoint,
		RequestedModel: requestedModel,
		UpstreamModel:  candidate.UpstreamName,
		HTTPStatus:     result.status,
		Stream:         stream,
		Tokens:         result.tokens,
		EstimatedCost:  cost,
		DurationMs:     time.Since(startedAt).Milliseconds(),
		FirstTokenMs:   result.firstTokenMs,
		Attempts:       attempts,
		ErrorMessage:   result.errorMessage,
	})
	if cost != nil && result.status >= 200 && result.status < 300 {
		_ = apikey.New(s.app).AddSpend(ctx, key, cost.InexactFloat64())
	}
}

func (s *Service) estimateCost(ctx context.Context, candidate Candidate, endpoint string, tokens usage.TokenUsage) *decimal.Decimal {
	switch candidate.BillingMode {
	case "rules":
		return usage.EstimateRuleCost(ctx, candidate.PublicName, endpoint, tokens)
	case "request":
		return usage.EstimateCost(tokens, usage.PriceRates{Request: candidate.RequestPrice})
	default:
		return usage.EstimateCost(tokens, usage.PriceRates{
			Input:       candidate.InputPrice,
			CachedInput: candidate.CachedInputPrice,
			CacheWrite:  candidate.CacheWritePrice,
			Output:      candidate.OutputPrice,
			ImageInput:  candidate.ImageInputPrice,
			AudioInput:  candidate.AudioInputPrice,
			AudioOutput: candidate.AudioOutputPrice,
		})
	}
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
