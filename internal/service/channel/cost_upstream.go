package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

func (s *Service) queryOpenAICosts(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, start, end time.Time, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	values := url.Values{}
	values.Set("start_time", strconv.FormatInt(start.Unix(), 10))
	values.Set("end_time", strconv.FormatInt(end.Unix(), 10))
	values.Set("bucket_width", "1d")
	values.Set("limit", "180")
	if channel.ProjectId != "" {
		values.Add("project_ids", channel.ProjectId)
	}
	parsed, _ := url.Parse(endpoint)
	parsed.RawQuery = values.Encode()
	body, err := s.getCostJSON(ctx, channel, credentialCipher, parsed.String(), config)
	if err != nil {
		return err
	}
	var payload struct {
		Data []struct {
			Results []struct {
				Amount *struct {
					Value    float64 `json:"value"`
					Currency string  `json:"currency"`
				} `json:"amount"`
			} `json:"results"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		return gerror.Wrap(err, "decode OpenAI costs")
	}
	used := 0.0
	for _, bucket := range payload.Data {
		for _, item := range bucket.Results {
			if item.Amount != nil {
				used += item.Amount.Value
				if item.Amount.Currency != "" {
					result.Currency = strings.ToUpper(item.Amount.Currency)
				}
			}
		}
	}
	result.UsedAmount = &used
	return nil
}

func (s *Service) querySub2API(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	body, err := s.getCostJSON(ctx, channel, credentialCipher, endpoint, config)
	if err != nil {
		return err
	}
	result.RemainingAmount = firstFloat(body, config.RemainingPath, "remaining", "balance", "quota.remaining")
	result.UsedAmount = firstFloat(body, config.UsedPath, "used", "usage.total.cost", "usage.total.actual_cost", "quota.used")
	if currency := firstString(body, config.CurrencyPath, "unit", "currency", "quota.unit"); currency != "" {
		result.Currency = strings.ToUpper(currency)
	}
	if result.RemainingAmount == nil && result.UsedAmount == nil {
		return gerror.New("Sub2API usage response did not contain supported cost fields")
	}
	return nil
}

func (s *Service) queryCustomJSON(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	body, err := s.getCostJSON(ctx, channel, credentialCipher, endpoint, config)
	if err != nil {
		return err
	}
	if config.UsedPath != "" {
		result.UsedAmount = jsonFloat(body, config.UsedPath)
	}
	if config.RemainingPath != "" {
		result.RemainingAmount = jsonFloat(body, config.RemainingPath)
	}
	if config.CurrencyPath != "" {
		result.Currency = strings.ToUpper(gjson.GetBytes(body, config.CurrencyPath).String())
	} else if config.FixedCurrency != "" {
		result.Currency = strings.ToUpper(config.FixedCurrency)
	}
	if result.UsedAmount == nil && result.RemainingAmount == nil {
		return gerror.New("custom cost query paths did not match numeric values")
	}
	return nil
}

func (s *Service) getCostJSON(ctx context.Context, channel entity.Channels, credentialCipher, endpoint string, config channeltype.CostConfig) ([]byte, error) {
	return s.fetchUpstreamJSON(ctx, channel, credentialCipher, upstreamJSONRequest{
		Method:       config.Method,
		Endpoint:     endpoint,
		AuthType:     config.AuthType,
		HeaderName:   config.HeaderName,
		HeaderPrefix: config.HeaderPrefix,
		BodyLimit:    4 << 20,
		RequestError: "create cost query request",
		FetchError:   "query upstream cost",
		ReadError:    "read upstream cost response",
		InvalidError: "upstream cost query returned invalid JSON",
		StatusError: func(status int, body []byte) error {
			return gerror.Newf("upstream cost query returned HTTP %d: %s", status, upstreamError(body, http.StatusText(status)))
		},
	})
}

func costRange(startDate, endDate string) (time.Time, time.Time, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := now
	var err error
	if startDate != "" {
		start, err = time.ParseInLocation("2006-01-02", startDate, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, gerror.New("startDate must use YYYY-MM-DD")
		}
	}
	if endDate != "" {
		end, err = time.ParseInLocation("2006-01-02", endDate, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, gerror.New("endDate must use YYYY-MM-DD")
		}
		end = end.Add(24 * time.Hour)
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, gerror.New("endDate must be after startDate")
	}
	return start, end, nil
}

func resolveEndpointURL(baseURL, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return "", gerror.New("configured endpoint URL is required")
	}
	parsed, err := url.Parse(configured)
	if err != nil {
		return "", gerror.Wrap(err, "parse configured endpoint URL")
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", gerror.New("configured endpoint URL must use HTTP(S)")
		}
		return parsed.String(), nil
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", gerror.New("channel base URL is required")
	}
	return baseURL + "/" + strings.TrimLeft(parsed.String(), "/"), nil
}

func firstFloat(body []byte, paths ...string) *float64 {
	for _, path := range paths {
		if value := jsonFloat(body, path); value != nil {
			return value
		}
	}
	return nil
}

func jsonFloat(body []byte, path string) *float64 {
	value := gjson.GetBytes(body, path)
	if !value.Exists() || (value.Type != gjson.Number && value.Type != gjson.String) {
		return nil
	}
	number, err := strconv.ParseFloat(value.String(), 64)
	if err != nil {
		return nil
	}
	return &number
}

func firstString(body []byte, paths ...string) string {
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}
