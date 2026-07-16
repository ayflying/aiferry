package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

type CostResult struct {
	Mode            string     `json:"mode"`
	UsedAmount      *float64   `json:"usedAmount"`
	RemainingAmount *float64   `json:"remainingAmount"`
	Currency        string     `json:"currency"`
	PeriodStart     *time.Time `json:"periodStart"`
	PeriodEnd       *time.Time `json:"periodEnd"`
	QueriedAt       time.Time  `json:"queriedAt"`
}

func (s *Service) QueryCost(ctx context.Context, channelID uint64, input adminapi.CostQueryInput) (CostResult, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return CostResult{}, err
	}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return CostResult{}, err
	}
	start, end, err := costRange(input.StartDate, input.EndDate)
	if err != nil {
		return CostResult{}, err
	}
	result := CostResult{Mode: config.Costs.Adapter, Currency: "USD", PeriodStart: &start, PeriodEnd: &end, QueriedAt: time.Now()}
	if config.Costs.FixedCurrency != "" {
		result.Currency = config.Costs.FixedCurrency
	}
	switch config.Costs.Adapter {
	case channeltype.AdapterOpenAICosts:
		err = s.queryOpenAICosts(ctx, channel, config.Costs, start, end, &result)
	case channeltype.AdapterSub2API:
		err = s.querySub2API(ctx, channel, config.Costs, &result)
	case channeltype.AdapterCustomJSON:
		err = s.queryCustomJSON(ctx, channel, config.Costs, &result)
	default:
		err = gerror.New("cost query is not configured")
	}
	if err != nil {
		return CostResult{}, err
	}
	if err = s.saveCostResult(ctx, channel.Id, result); err != nil {
		return CostResult{}, err
	}
	return result, nil
}

func (s *Service) queryOpenAICosts(ctx context.Context, channel entity.Channels, config channeltype.CostConfig, start, end time.Time, result *CostResult) error {
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
	body, err := s.getCostJSON(ctx, channel, parsed.String(), config)
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

func (s *Service) querySub2API(ctx context.Context, channel entity.Channels, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	body, err := s.getCostJSON(ctx, channel, endpoint, config)
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

func (s *Service) queryCustomJSON(ctx context.Context, channel entity.Channels, config channeltype.CostConfig, result *CostResult) error {
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Path)
	if err != nil {
		return err
	}
	body, err := s.getCostJSON(ctx, channel, endpoint, config)
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

func (s *Service) getCostJSON(ctx context.Context, channel entity.Channels, endpoint string, config channeltype.CostConfig) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, config.Method, endpoint, nil)
	if err != nil {
		return nil, gerror.Wrap(err, "create cost query request")
	}
	if err = s.setConfiguredHeaders(ctx, req, channel, config.AuthType, config.HeaderName, config.HeaderPrefix); err != nil {
		return nil, err
	}
	resp, err := s.app.HTTP.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, "query upstream cost")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, gerror.Wrap(err, "read upstream cost response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gerror.Newf("upstream cost query returned HTTP %d: %s", resp.StatusCode, upstreamError(body, resp.Status))
	}
	if !gjson.ValidBytes(body) {
		return nil, gerror.New("upstream cost query returned invalid JSON")
	}
	return body, nil
}

func (s *Service) saveCostResult(ctx context.Context, channelID uint64, result CostResult) error {
	snapshot := do.ChannelCostSnapshots{
		ChannelId: channelID,
		Mode:      result.Mode,
		Currency:  result.Currency,
		QueriedAt: result.QueriedAt,
	}
	channelUpdate := do.Channels{LastCostCurrency: result.Currency, LastCostAt: result.QueriedAt}
	if result.UsedAmount == nil {
		snapshot.UsedAmount = gdb.Raw("NULL")
		channelUpdate.LastCostUsed = gdb.Raw("NULL")
	} else {
		snapshot.UsedAmount = *result.UsedAmount
		channelUpdate.LastCostUsed = *result.UsedAmount
	}
	if result.RemainingAmount == nil {
		snapshot.RemainingAmount = gdb.Raw("NULL")
		channelUpdate.LastCostRemaining = gdb.Raw("NULL")
	} else {
		snapshot.RemainingAmount = *result.RemainingAmount
		channelUpdate.LastCostRemaining = *result.RemainingAmount
	}
	if result.PeriodStart != nil {
		snapshot.PeriodStart = *result.PeriodStart
	}
	if result.PeriodEnd != nil {
		snapshot.PeriodEnd = *result.PeriodEnd
	}
	if _, err := dao.ChannelCostSnapshots.Ctx(ctx).Data(snapshot).Insert(); err != nil {
		return gerror.Wrap(err, "save cost snapshot")
	}
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, channelID).Data(channelUpdate).Update(); err != nil {
		return gerror.Wrap(err, "update channel cost snapshot")
	}
	return nil
}

func costRange(startDate, endDate string) (time.Time, time.Time, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
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
