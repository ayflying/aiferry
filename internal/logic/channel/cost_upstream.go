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

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/upstreamerror"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) queryOpenAICosts(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, start, end time.Time, result *CostResult) error {
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

func qiniuUsageRange(start, end time.Time) (time.Time, time.Time) {
	if end.Sub(start) > 31*24*time.Hour {
		start = end.Add(-31 * 24 * time.Hour)
	}
	return start, end
}

func (s *sChannel) queryQiniuCosts(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	parsed, _ := url.Parse(endpoint)
	values := parsed.Query()
	values.Set("type", "day")
	values.Set("timezone", "Asia/Shanghai")
	parsed.RawQuery = values.Encode()
	body, err := s.getCostJSON(ctx, channel, credentialCipher, parsed.String(), config)
	if err != nil {
		return err
	}
	total, err := parseQiniuCosts(body)
	if err != nil {
		return err
	}
	result.UsedAmount = &total
	return nil
}

func parseQiniuCosts(body []byte) (float64, error) {
	var payload struct {
		Status bool   `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			APIKeys []struct {
				TotalFee float64 `json:"total_fee"`
			} `json:"api_keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, gerror.Wrap(err, "decode Qiniu costs")
	}
	if !payload.Status {
		if payload.Error == "" {
			payload.Error = "unknown error"
		}
		return 0, gerror.New("Qiniu cost query failed: " + payload.Error)
	}
	var total float64
	for _, key := range payload.Data.APIKeys {
		total += key.TotalFee
	}
	return total, nil
}

func (s *sChannel) queryQiniuUsage(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, start, end time.Time, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	start, end = qiniuUsageRange(start, end)
	values := url.Values{}
	values.Set("granularity", "day")
	values.Set("start", start.Format(time.RFC3339))
	values.Set("end", end.Format(time.RFC3339))
	values.Set("timezone", "Asia/Shanghai")
	parsed, _ := url.Parse(endpoint)
	parsed.RawQuery = values.Encode()
	body, err := s.getCostJSON(ctx, channel, credentialCipher, parsed.String(), config)
	if err != nil {
		return err
	}
	total, unit, err := parseQiniuUsage(body)
	if err != nil {
		return err
	}
	result.Currency = "TOKEN"
	result.Usage = &total
	result.UsageUnit = unit
	result.UsageType = "Token"
	result.UsageDimension = "全部模型"
	// Existing cost snapshot columns persist the numeric usage through UsedAmount.
	result.UsedAmount = &total
	return nil
}

func parseQiniuUsage(body []byte) (float64, string, error) {
	var payload struct {
		Status bool   `json:"status"`
		Error  string `json:"error"`
		Data   []struct {
			Items []struct {
				Name  string  `json:"name"`
				Unit  string  `json:"unit"`
				Total float64 `json:"total"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, "", gerror.Wrap(err, "decode Qiniu usage")
	}
	if !payload.Status {
		if payload.Error == "" {
			payload.Error = "unknown error"
		}
		return 0, "", gerror.New("Qiniu usage query failed: " + payload.Error)
	}
	var total float64
	unit := "kToken"
	foundTokenItem := false
	for _, bucket := range payload.Data {
		for _, item := range bucket.Items {
			if !isQiniuTokenItem(item.Name, item.Unit) {
				continue
			}
			if item.Unit != "" && foundTokenItem && item.Unit != unit {
				continue
			}
			total += item.Total
			if item.Unit != "" {
				unit = item.Unit
			}
			foundTokenItem = true
		}
	}
	return total, unit, nil
}

func isQiniuTokenItem(name, unit string) bool {
	value := strings.ToLower(strings.TrimSpace(name + " " + unit))
	return strings.Contains(value, "token") || strings.Contains(value, "令牌")
}

func (s *sChannel) querySub2API(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	body, err := s.getCostJSON(ctx, channel, credentialCipher, endpoint, config)
	if err != nil {
		return err
	}
	if channeltype.IsUsageCost(config) {
		result.Usage = firstFloat(body, config.UsagePath, config.UsedPath, "usage", "total", "data.usage")
		if result.Usage == nil {
			return gerror.New("Sub2API usage response did not contain supported usage fields")
		}
		result.UsedAmount = result.Usage
		result.UsageUnit = config.UsageUnit
		if result.UsageUnit == "" {
			result.UsageUnit = "kToken"
		}
		result.UsageType = config.UsageType
		if result.UsageType == "" {
			result.UsageType = "用量"
		}
		result.UsageDimension = config.UsageDimension
		return nil
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

func (s *sChannel) queryCustomJSON(ctx context.Context, channel entity.Channels, credentialCipher string, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	body, err := s.getCostJSON(ctx, channel, credentialCipher, endpoint, config)
	if err != nil {
		return err
	}
	if channeltype.IsUsageCost(config) {
		result.Usage = jsonFloat(body, config.UsagePath)
		if result.Usage == nil && config.UsagePath == "" {
			result.Usage = jsonFloat(body, config.UsedPath)
		}
		result.UsedAmount = result.Usage
		result.UsageUnit = config.UsageUnit
		if result.UsageUnit == "" {
			result.UsageUnit = "kToken"
		}
		result.UsageType = config.UsageType
		if result.UsageType == "" {
			result.UsageType = "用量"
		}
		result.UsageDimension = config.UsageDimension
	} else if config.UsedPath != "" {
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

func (s *sChannel) getCostJSON(ctx context.Context, channel entity.Channels, credentialCipher, endpoint string, config channeltype.CostConfig) ([]byte, error) {
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
			return gerror.Newf("upstream cost query returned HTTP %d: %s", status, upstreamerror.Message(body, http.StatusText(status)))
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
	if strings.TrimSpace(path) == "" {
		return nil
	}
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
