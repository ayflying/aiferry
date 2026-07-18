package channel

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

func (s *Service) SyncAllPrices(ctx context.Context) (PriceSyncResult, error) {
	var channels []entity.Channels
	if err := dao.Channels.Ctx(ctx).
		Where(dao.Channels.Columns().Status, 1).
		OrderAsc(dao.Channels.Columns().Priority).
		OrderAsc(dao.Channels.Columns().Id).
		Scan(&channels); err != nil {
		return PriceSyncResult{}, gerror.Wrap(err, "list price sync channels")
	}

	result := PriceSyncResult{Failures: make([]PriceSyncSourceFailure, 0)}
	for _, channel := range channels {
		_, config, err := s.types.GetByCode(ctx, channel.Type)
		if err != nil {
			result.Sources++
			result.Failures = append(result.Failures, priceSyncFailure("channel", channel.Id, channel.Name, err))
			continue
		}
		if config.Pricing.Adapter == channeltype.AdapterNone {
			continue
		}
		result.Sources++
		count, err := s.syncPricesFromChannel(ctx, channel, config)
		if err != nil {
			result.Failures = append(result.Failures, priceSyncFailure("channel", channel.Id, channel.Name, err))
			continue
		}
		result.Count += count
		result.Succeeded++
	}
	return result, nil
}

func (s *Service) SyncPriceSource(ctx context.Context, channelID uint64) (PriceSyncResult, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return PriceSyncResult{}, err
	}
	result := PriceSyncResult{Sources: 1, Failures: make([]PriceSyncSourceFailure, 0)}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		result.Failures = append(result.Failures, priceSyncFailure("channel", channel.Id, channel.Name, err))
		return result, nil
	}
	if config.Pricing.Adapter == channeltype.AdapterNone {
		result.Failures = append(result.Failures, priceSyncFailure("channel", channel.Id, channel.Name, gerror.New("渠道类型没有配置价格同步接口")))
		return result, nil
	}
	count, err := s.syncPricesFromChannel(ctx, channel, config)
	if err != nil {
		result.Failures = append(result.Failures, priceSyncFailure("channel", channel.Id, channel.Name, err))
		return result, nil
	}
	result.Count = count
	result.Succeeded = 1
	return result, nil
}

func (s *Service) SyncExternalPriceSource(ctx context.Context, sourceID uint64, sourceName, baseURL string, config channeltype.PricingConfig) (PriceSyncResult, error) {
	result := PriceSyncResult{Sources: 1, Failures: make([]PriceSyncSourceFailure, 0)}
	endpoint, err := resolveEndpointURL(baseURL, config.Path)
	if err == nil && config.AuthType != channeltype.AuthNone {
		err = gerror.New("public price sources currently support only pricing.authType none")
	}
	if err == nil {
		body, fetchErr := s.fetchPublicJSON(ctx, upstreamJSONRequest{
			Method:       config.Method,
			Endpoint:     endpoint,
			BodyLimit:    8 << 20,
			RequestError: "create public price source request",
			FetchError:   "fetch public price source",
			ReadError:    "read public price source response",
			InvalidError: "public price source returned invalid JSON",
			StatusError: func(status int, _ []byte) error {
				return gerror.Newf("public price source returned HTTP %d", status)
			},
		})
		if fetchErr != nil {
			err = fetchErr
		} else {
			var count int
			count, err = s.syncPricesFromPayload(ctx, endpoint, config, body)
			if err == nil {
				result.Count = count
				result.Succeeded = 1
			}
		}
	}
	if err != nil {
		result.Failures = append(result.Failures, priceSyncFailure("price_source", sourceID, sourceName, err))
	}
	return result, nil
}

func (s *Service) syncPricesFromChannel(ctx context.Context, channel entity.Channels, config channeltype.Config) (int, error) {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Pricing.Path)
	if err != nil {
		return 0, err
	}
	credential, err := s.CredentialForTest(ctx, channel.Id, 0)
	if err != nil && config.Pricing.AuthType != channeltype.AuthManagementKey && config.Pricing.AuthType != channeltype.AuthNone {
		return 0, err
	}
	body, err := s.fetchUpstreamJSON(ctx, channel, credential.APIKeyCipher, upstreamJSONRequest{
		Method:       config.Pricing.Method,
		Endpoint:     endpoint,
		AuthType:     config.Pricing.AuthType,
		HeaderName:   config.Pricing.HeaderName,
		HeaderPrefix: config.Pricing.HeaderPrefix,
		BodyLimit:    8 << 20,
		RequestError: "create price sync request",
		FetchError:   "fetch upstream prices",
		ReadError:    "read upstream prices",
		InvalidError: "upstream price query returned invalid JSON",
		StatusError: func(status int, _ []byte) error {
			return gerror.Newf("upstream price query returned HTTP %d", status)
		},
	})
	if err != nil {
		return 0, err
	}
	return s.syncPricesFromPayload(ctx, endpoint, config.Pricing, body)
}

func priceSyncFailure(kind string, id uint64, name string, err error) PriceSyncSourceFailure {
	failure := PriceSyncSourceFailure{SourceKind: kind, SourceID: id, SourceName: name, Message: err.Error()}
	if kind == "channel" {
		failure.ChannelID = id
		failure.ChannelName = name
	}
	return failure
}
