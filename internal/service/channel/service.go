package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/app"
)

const (
	ModeNone        = "none"
	ModeOpenAICosts = "openai_costs"
	ModeSub2API     = "sub2api_usage"
	ModeCustomJSON  = "custom_json"
)

type Service struct {
	app *app.Service
}

type View struct {
	Id                uint64                   `json:"id"`
	Name              string                   `json:"name"`
	Type              string                   `json:"type"`
	BaseURL           string                   `json:"baseUrl"`
	HasAPIKey         bool                     `json:"hasApiKey"`
	HasManagementKey  bool                     `json:"hasManagementKey"`
	OrganizationID    string                   `json:"organizationId"`
	ProjectID         string                   `json:"projectId"`
	Status            int                      `json:"status"`
	Priority          int                      `json:"priority"`
	Weight            uint                     `json:"weight"`
	CostQueryMode     string                   `json:"costQueryMode"`
	CostQueryConfig   adminapi.CostQueryConfig `json:"costQueryConfig"`
	EnabledModelCount int                      `json:"enabledModelCount"`
	DiscoveredModels  int                      `json:"discoveredModels"`
	LastTestStatus    string                   `json:"lastTestStatus"`
	LastTestLatencyMs uint                     `json:"lastTestLatencyMs"`
	LastTestError     string                   `json:"lastTestError"`
	LastTestAt        *time.Time               `json:"lastTestAt"`
	LastCostUsed      *float64                 `json:"lastCostUsed"`
	LastCostRemaining *float64                 `json:"lastCostRemaining"`
	LastCostCurrency  string                   `json:"lastCostCurrency"`
	LastCostAt        *time.Time               `json:"lastCostAt"`
	CreatedAt         time.Time                `json:"createdAt"`
}

type ModelView struct {
	Id                uint64     `json:"id" orm:"id"`
	ChannelId         uint64     `json:"channelId" orm:"channel_id"`
	ChannelName       string     `json:"channelName" orm:"channel_name"`
	PublicName        string     `json:"publicName" orm:"public_name"`
	UpstreamName      string     `json:"upstreamName" orm:"upstream_name"`
	Discovered        int        `json:"discovered" orm:"discovered"`
	Enabled           int        `json:"enabled" orm:"enabled"`
	InputPrice        *float64   `json:"inputPrice" orm:"input_price"`
	CachedInputPrice  *float64   `json:"cachedInputPrice" orm:"cached_input_price"`
	OutputPrice       *float64   `json:"outputPrice" orm:"output_price"`
	LastTestEndpoint  string     `json:"lastTestEndpoint" orm:"last_test_endpoint"`
	LastTestStatus    string     `json:"lastTestStatus" orm:"last_test_status"`
	LastTestLatencyMs uint       `json:"lastTestLatencyMs" orm:"last_test_latency_ms"`
	LastTestError     string     `json:"lastTestError" orm:"last_test_error"`
	LastTestAt        *time.Time `json:"lastTestAt" orm:"last_test_at"`
	UpdatedAt         time.Time  `json:"updatedAt" orm:"updated_at"`
}

func New(appSvc *app.Service) *Service {
	return &Service{app: appSvc}
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	var rows []entity.Channels
	if err := dao.Channels.Ctx(ctx).OrderDesc(dao.Channels.Columns().Priority).OrderDesc(dao.Channels.Columns().Id).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list channels")
	}
	views := make([]View, 0, len(rows))
	for i := range rows {
		view := s.toView(rows[i])
		view.DiscoveredModels, _ = dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().ChannelId, rows[i].Id).Count()
		view.EnabledModelCount, _ = dao.ChannelModels.Ctx(ctx).
			Where(do.ChannelModels{ChannelId: rows[i].Id, Enabled: 1}).Count()
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Get(ctx context.Context, id uint64) (entity.Channels, error) {
	var row entity.Channels
	if err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, id).Scan(&row); err != nil {
		return row, gerror.Wrap(err, "find channel")
	}
	if row.Id == 0 {
		return row, gerror.New("channel not found")
	}
	return row, nil
}

func (s *Service) Create(ctx context.Context, input adminapi.ChannelInput) (uint64, error) {
	if input.APIKey == nil || strings.TrimSpace(*input.APIKey) == "" {
		return 0, gerror.New("API key is required")
	}
	baseURL, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return 0, err
	}
	apiKeyCipher, err := s.app.Secrets.Encrypt(strings.TrimSpace(*input.APIKey))
	if err != nil {
		return 0, err
	}
	data := do.Channels{
		Name:            strings.TrimSpace(input.Name),
		Type:            "openai",
		BaseUrl:         baseURL,
		ApiKeyCipher:    apiKeyCipher,
		OrganizationId:  strings.TrimSpace(input.OrganizationID),
		ProjectId:       strings.TrimSpace(input.ProjectID),
		Status:          boolStatus(input.Status),
		Priority:        input.Priority,
		Weight:          normalizeWeight(input.Weight),
		CostQueryMode:   normalizeCostMode(input.CostQueryMode),
		CostQueryConfig: encodeCostConfig(input.CostQueryConfig),
	}
	if input.ManagementKey != nil && strings.TrimSpace(*input.ManagementKey) != "" {
		data.ManagementKeyCipher, err = s.app.Secrets.Encrypt(strings.TrimSpace(*input.ManagementKey))
		if err != nil {
			return 0, err
		}
	}
	id, err := dao.Channels.Ctx(ctx).Data(data).InsertAndGetId()
	if err != nil {
		return 0, gerror.Wrap(err, "create channel")
	}
	return uint64(id), nil
}

func (s *Service) Update(ctx context.Context, id uint64, input adminapi.ChannelInput) error {
	current, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	baseURL, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return err
	}
	data := do.Channels{
		Name:            strings.TrimSpace(input.Name),
		BaseUrl:         baseURL,
		OrganizationId:  strings.TrimSpace(input.OrganizationID),
		ProjectId:       strings.TrimSpace(input.ProjectID),
		Status:          boolStatus(input.Status),
		Priority:        input.Priority,
		Weight:          normalizeWeight(input.Weight),
		CostQueryMode:   normalizeCostMode(input.CostQueryMode),
		CostQueryConfig: encodeCostConfig(input.CostQueryConfig),
	}
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		data.ApiKeyCipher, err = s.app.Secrets.Encrypt(strings.TrimSpace(*input.APIKey))
		if err != nil {
			return err
		}
	}
	if input.ManagementKey != nil {
		if strings.TrimSpace(*input.ManagementKey) == "" {
			data.ManagementKeyCipher = gdb.Raw("NULL")
		} else {
			data.ManagementKeyCipher, err = s.app.Secrets.Encrypt(strings.TrimSpace(*input.ManagementKey))
			if err != nil {
				return err
			}
		}
	}
	if _, err = dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, current.Id).Data(data).Update(); err != nil {
		return gerror.Wrap(err, "update channel")
	}
	return s.invalidateRoutes(ctx)
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, id).Data(do.Channels{Status: 0}).Update(); err != nil {
		return gerror.Wrap(err, "disable channel before delete")
	}
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, id).Delete(); err != nil {
		return gerror.Wrap(err, "delete channel")
	}
	return s.invalidateRoutes(ctx)
}

func (s *Service) DiscoverModels(ctx context.Context, channelID uint64) ([]string, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return nil, err
	}
	apiKey, err := s.app.Secrets.Decrypt(channel.ApiKeyCipher)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, channel.BaseUrl+"/models", nil)
	if err != nil {
		return nil, gerror.Wrap(err, "create model discovery request")
	}
	setUpstreamHeaders(req, channel, apiKey)
	resp, err := s.app.HTTP.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, "fetch upstream models")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, gerror.Newf("upstream model query returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, gerror.Wrap(err, "decode upstream models")
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		name := strings.TrimSpace(item.ID)
		if name == "" {
			continue
		}
		models = append(models, name)
		count, countErr := dao.ChannelModels.Ctx(ctx).
			Where(do.ChannelModels{ChannelId: channelID, UpstreamName: name}).Count()
		if countErr != nil {
			return nil, gerror.Wrap(countErr, "check discovered model")
		}
		if count == 0 {
			_, err = dao.ChannelModels.Ctx(ctx).Data(do.ChannelModels{
				ChannelId:    channelID,
				PublicName:   name,
				UpstreamName: name,
				Discovered:   1,
				Enabled:      0,
			}).Insert()
			if err != nil {
				return nil, gerror.Wrap(err, "save discovered model")
			}
		}
	}
	return models, nil
}

func (s *Service) ListModels(ctx context.Context, channelID uint64) ([]ModelView, error) {
	var rows []ModelView
	model := dao.ChannelModels.Ctx(ctx).As("m").
		Fields("m.*,c.name AS channel_name").
		LeftJoin(dao.Channels.Table()+" c", "c.id=m.channel_id")
	if channelID > 0 {
		model = model.Where("m.channel_id", channelID)
	}
	err := model.OrderDesc("m.enabled").OrderAsc("m.public_name").Scan(&rows)
	return rows, gerror.Wrap(err, "list channel models")
}

func (s *Service) UpdateModel(ctx context.Context, id uint64, input adminapi.ModelInput) error {
	data := do.ChannelModels{
		PublicName:   strings.TrimSpace(input.PublicName),
		UpstreamName: strings.TrimSpace(input.UpstreamName),
		Enabled:      boolInt(input.Enabled),
	}
	data.InputPrice = nullableNumber(input.InputPrice)
	data.CachedInputPrice = nullableNumber(input.CachedInputPrice)
	data.OutputPrice = nullableNumber(input.OutputPrice)
	result, err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, id).Data(data).Update()
	if err != nil {
		return gerror.Wrap(err, "update channel model")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return gerror.New("model not found")
	}
	return s.invalidateRoutes(ctx)
}

func (s *Service) toView(row entity.Channels) View {
	view := View{
		Id:                row.Id,
		Name:              row.Name,
		Type:              row.Type,
		BaseURL:           row.BaseUrl,
		HasAPIKey:         row.ApiKeyCipher != "",
		HasManagementKey:  row.ManagementKeyCipher != "",
		OrganizationID:    row.OrganizationId,
		ProjectID:         row.ProjectId,
		Status:            row.Status,
		Priority:          row.Priority,
		Weight:            row.Weight,
		CostQueryMode:     row.CostQueryMode,
		LastTestStatus:    row.LastTestStatus,
		LastTestLatencyMs: row.LastTestLatencyMs,
		LastTestError:     row.LastTestError,
		LastCostCurrency:  row.LastCostCurrency,
		CreatedAt:         row.CreatedAt,
	}
	_ = json.Unmarshal([]byte(row.CostQueryConfig), &view.CostQueryConfig)
	if !row.LastTestAt.IsZero() {
		view.LastTestAt = &row.LastTestAt
	}
	if !row.LastCostAt.IsZero() {
		view.LastCostAt = &row.LastCostAt
		view.LastCostUsed = &row.LastCostUsed
		view.LastCostRemaining = &row.LastCostRemaining
	}
	return view
}

func normalizeBaseURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", gerror.New("baseUrl must be an absolute HTTP(S) URL")
	}
	if !strings.HasSuffix(parsed.Path, "/v1") {
		return "", gerror.New("baseUrl must end with /v1")
	}
	return value, nil
}

func setUpstreamHeaders(req *http.Request, channel entity.Channels, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	if channel.OrganizationId != "" {
		req.Header.Set("OpenAI-Organization", channel.OrganizationId)
	}
	if channel.ProjectId != "" {
		req.Header.Set("OpenAI-Project", channel.ProjectId)
	}
}

func boolStatus(value int) int {
	if value == 0 {
		return 0
	}
	return 1
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeWeight(value uint) uint {
	if value == 0 {
		return 1
	}
	return value
}

func normalizeCostMode(value string) string {
	switch value {
	case ModeOpenAICosts, ModeSub2API, ModeCustomJSON:
		return value
	default:
		return ModeNone
	}
}

func encodeCostConfig(config adminapi.CostQueryConfig) string {
	encoded, _ := json.Marshal(config)
	return string(encoded)
}

func nullableNumber(value *float64) any {
	if value == nil {
		return gdb.Raw("NULL")
	}
	return *value
}

func (s *Service) invalidateRoutes(ctx context.Context) error {
	return s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
}

func routeCacheKey(model string) string {
	return fmt.Sprintf("aiferry:routes:%s", model)
}
