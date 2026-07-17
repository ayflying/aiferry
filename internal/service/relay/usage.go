package relay

import (
	"context"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/dao"
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
		return s.estimateRuleCost(ctx, candidate.PublicName, endpoint, tokens)
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

func (s *Service) estimateRuleCost(ctx context.Context, modelName, endpoint string, tokens usage.TokenUsage) *decimal.Decimal {
	var rules []struct {
		ConditionsJSON string `orm:"conditions_json"`
		RatesJSON      string `orm:"rates_json"`
	}
	err := dao.ModelPriceRules.Ctx(ctx).
		Fields("conditions_json,rates_json").
		Where("model_name", modelName).
		Where("status", 1).
		OrderDesc("priority").
		OrderDesc("source = 'manual'").
		OrderDesc("id").
		Scan(&rules)
	if err == nil {
		for _, rule := range rules {
			if cost, ok := ruleCost(rule.ConditionsJSON, rule.RatesJSON, endpoint, tokens); ok {
				return cost
			}
		}
	}
	return nil
}

func ruleCost(conditionsJSON, ratesJSON, endpoint string, tokens usage.TokenUsage) (*decimal.Decimal, bool) {
	conditions := gjson.Parse(conditionsJSON)
	if configured := strings.TrimSpace(conditions.Get("endpoint").String()); configured != "" && configured != endpoint {
		return nil, false
	}
	input := uint64(0)
	if tokens.Input != nil {
		input = *tokens.Input
	}
	if min := conditions.Get("inputTokensAtLeast"); min.Exists() && input < min.Uint() {
		return nil, false
	}
	if max := conditions.Get("inputTokensAtMost"); max.Exists() && input > max.Uint() {
		return nil, false
	}
	output := uint64(0)
	if tokens.Output != nil {
		output = *tokens.Output
	}
	if min := conditions.Get("outputTokensAtLeast"); min.Exists() && output < min.Uint() {
		return nil, false
	}
	if max := conditions.Get("outputTokensAtMost"); max.Exists() && output > max.Uint() {
		return nil, false
	}
	total := input + output
	if min := conditions.Get("totalTokensAtLeast"); min.Exists() && total < min.Uint() {
		return nil, false
	}
	if max := conditions.Get("totalTokensAtMost"); max.Exists() && total > max.Uint() {
		return nil, false
	}
	rates := gjson.Parse(ratesJSON)
	cost := usage.EstimateCost(tokens, usage.PriceRates{
		Input:       rateValue(rates.Get("inputPerMillion")),
		CachedInput: rateValue(rates.Get("cachedInputPerMillion")),
		CacheWrite:  rateValue(rates.Get("cacheWritePerMillion")),
		Output:      rateValue(rates.Get("outputPerMillion")),
		ImageInput:  rateValue(rates.Get("imageInputPerMillion")),
		AudioInput:  rateValue(rates.Get("audioInputPerMillion")),
		AudioOutput: rateValue(rates.Get("audioOutputPerMillion")),
		Request:     rateValue(rates.Get("request")),
	})
	return cost, cost != nil
}

func rateValue(value gjson.Result) *float64 {
	if !value.Exists() || value.Type != gjson.Number {
		return nil
	}
	result := value.Float()
	return &result
}

func parseJSONUsage(body []byte) usage.TokenUsage {
	input := optionalUint(body, "usage.input_tokens", "usage.prompt_tokens", "response.usage.input_tokens")
	cached := optionalUint(body, "usage.input_tokens_details.cached_tokens", "usage.prompt_tokens_details.cached_tokens", "response.usage.input_tokens_details.cached_tokens")
	cacheWrite := optionalUint(body, "usage.cache_creation_input_tokens", "usage.cache_creation_tokens", "usage.input_tokens_details.cache_creation_tokens", "usage.prompt_tokens_details.cache_creation_tokens")
	imageInput := optionalUint(body, "usage.image_tokens", "usage.input_tokens_details.image_tokens", "usage.prompt_tokens_details.image_tokens")
	audioInput := optionalUint(body, "usage.audio_tokens", "usage.input_tokens_details.audio_tokens", "usage.prompt_tokens_details.audio_tokens")
	output := optionalUint(body, "usage.output_tokens", "usage.completion_tokens", "response.usage.output_tokens")
	audioOutput := optionalUint(body, "usage.output_audio_tokens", "usage.output_tokens_details.audio_tokens", "usage.completion_tokens_details.audio_tokens")
	total := optionalUint(body, "usage.total_tokens", "response.usage.total_tokens")
	if total == nil && input != nil && output != nil {
		value := *input + *output
		total = &value
	}
	return usage.TokenUsage{Input: input, CachedInput: cached, CacheWrite: cacheWrite, ImageInput: imageInput, AudioInput: audioInput, Output: output, AudioOutput: audioOutput, Total: total}
}

func parseSSEUsage(line []byte, target *usage.TokenUsage) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if payload == "" || payload == "[DONE]" || !gjson.Valid(payload) {
		return
	}
	parsed := parseJSONUsage([]byte(payload))
	if parsed.Input != nil || parsed.CachedInput != nil || parsed.CacheWrite != nil || parsed.ImageInput != nil || parsed.AudioInput != nil || parsed.Output != nil || parsed.AudioOutput != nil {
		*target = parsed
	}
}

func optionalUint(body []byte, paths ...string) *uint64 {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Exists() && value.Type == gjson.Number {
			number := value.Uint()
			return &number
		}
	}
	return nil
}
