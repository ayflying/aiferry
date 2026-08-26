package channel

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) queryOpenRouterCredits(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	body, err := s.getCostJSON(ctx, channel, credentialCipher, endpoint, config)
	if err != nil {
		return err
	}
	result.UsedAmount, result.RemainingAmount, err = parseOpenRouterCredits(body)
	return err
}

func parseOpenRouterCredits(body []byte) (*float64, *float64, error) {
	total := jsonFloat(body, "data.total_credits")
	used := jsonFloat(body, "data.total_usage")
	if total == nil || used == nil {
		return nil, nil, gerror.New("OpenRouter credits response did not contain total_credits and total_usage")
	}
	remaining := *total - *used
	return used, &remaining, nil
}
