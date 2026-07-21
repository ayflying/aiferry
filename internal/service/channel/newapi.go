package channel

import (
	"context"
	"net/url"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

func (s *Service) queryNewAPI(ctx context.Context, channel entity.Channels, config channeltype.CostConfig, result *CostResult) error {
	accountEndpoint, err := newAPIEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	account, err := s.getCostJSON(ctx, channel, "", accountEndpoint, config)
	if err != nil {
		return err
	}
	statusEndpoint, err := newAPIEndpointURL(channel.BaseUrl, "/api/status")
	if err != nil {
		return err
	}
	statusConfig := config
	statusConfig.AuthType = channeltype.AuthNone
	statusConfig.HeaderName = ""
	statusConfig.HeaderPrefix = ""
	status, err := s.getCostJSON(ctx, channel, "", statusEndpoint, statusConfig)
	if err != nil {
		return err
	}
	used, remaining, err := newAPICostAmounts(account, status)
	if err != nil {
		return err
	}
	result.UsedAmount = used
	result.RemainingAmount = remaining
	return nil
}

func newAPIEndpointURL(baseURL, endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", gerror.New("NewAPI channel base URL must be an absolute HTTP(S) URL ending in /v1")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/v1") {
		return "", gerror.New("NewAPI channel base URL must end in /v1")
	}
	parsed.Path = strings.TrimSuffix(path, "/v1") + "/" + strings.TrimLeft(endpoint, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func newAPICostAmounts(account, status []byte) (*float64, *float64, error) {
	if !newAPIResponseSucceeded(account) {
		return nil, nil, gerror.New("NewAPI account query was not successful")
	}
	if !newAPIResponseSucceeded(status) {
		return nil, nil, gerror.New("NewAPI status query was not successful")
	}
	usedQuota := jsonFloat(account, "data.used_quota")
	remainingQuota := jsonFloat(account, "data.quota")
	if usedQuota == nil || remainingQuota == nil {
		return nil, nil, gerror.New("NewAPI account response did not contain quota values")
	}
	quotaPerUnit := gjson.GetBytes(status, "data.quota_per_unit").Float()
	if quotaPerUnit <= 0 {
		return nil, nil, gerror.New("NewAPI status response did not contain a valid quota_per_unit")
	}
	used := *usedQuota / quotaPerUnit
	remaining := *remainingQuota / quotaPerUnit
	return &used, &remaining, nil
}

func newAPIResponseSucceeded(body []byte) bool {
	value := gjson.GetBytes(body, "success")
	return !value.Exists() || value.Bool()
}
