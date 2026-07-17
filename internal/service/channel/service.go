package channel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/channelgroup"
	"github.com/yunloli/aiferry/internal/service/channeltype"
	"github.com/yunloli/aiferry/internal/service/system"
)

const (
	ModeNone        = "none"
	ModeOpenAICosts = "openai_costs"
	ModeSub2API     = "sub2api_usage"
	ModeCustomJSON  = "custom_json"
)

type Service struct {
	app        *app.Service
	types      *channeltype.Service
	groups     *channelgroup.Service
	resilience *system.Service
}

type View struct {
	Id                     uint64     `json:"id"`
	Name                   string     `json:"name"`
	Type                   string     `json:"type"`
	TypeName               string     `json:"typeName"`
	BaseURL                string     `json:"baseUrl"`
	HasAPIKey              bool       `json:"hasApiKey"`
	HasManagementKey       bool       `json:"hasManagementKey"`
	OrganizationID         string     `json:"organizationId"`
	ProjectID              string     `json:"projectId"`
	Status                 int        `json:"status"`
	AutoDisabled           bool       `json:"autoDisabled"`
	AutoDisabledAt         *time.Time `json:"autoDisabledAt"`
	AutoDisabledReason     string     `json:"autoDisabledReason"`
	AutoDisabledStatusCode *uint      `json:"autoDisabledStatusCode"`
	Priority               int        `json:"priority"`
	Weight                 uint       `json:"weight"`
	CostQueryMode          string     `json:"costQueryMode"`
	EnabledModelCount      int        `json:"enabledModelCount"`
	DiscoveredModels       int        `json:"discoveredModels"`
	LastTestStatus         string     `json:"lastTestStatus"`
	LastTestLatencyMs      uint       `json:"lastTestLatencyMs"`
	LastTestError          string     `json:"lastTestError"`
	LastTestAt             *time.Time `json:"lastTestAt"`
	LastCostUsed           *float64   `json:"lastCostUsed"`
	LastCostRemaining      *float64   `json:"lastCostRemaining"`
	LastCostCurrency       string     `json:"lastCostCurrency"`
	LastCostAt             *time.Time `json:"lastCostAt"`
	GroupIDs               []uint64   `json:"groupIds"`
	CreatedAt              time.Time  `json:"createdAt"`
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
	CacheWritePrice   *float64   `json:"cacheWritePrice" orm:"cache_write_price"`
	OutputPrice       *float64   `json:"outputPrice" orm:"output_price"`
	ImageInputPrice   *float64   `json:"imageInputPrice" orm:"image_input_price"`
	AudioInputPrice   *float64   `json:"audioInputPrice" orm:"audio_input_price"`
	AudioOutputPrice  *float64   `json:"audioOutputPrice" orm:"audio_output_price"`
	RequestPrice      *float64   `json:"requestPrice" orm:"request_price"`
	BillingMode       string     `json:"billingMode" orm:"billing_mode"`
	LastTestEndpoint  string     `json:"lastTestEndpoint" orm:"last_test_endpoint"`
	LastTestStatus    string     `json:"lastTestStatus" orm:"last_test_status"`
	LastTestLatencyMs uint       `json:"lastTestLatencyMs" orm:"last_test_latency_ms"`
	LastTestError     string     `json:"lastTestError" orm:"last_test_error"`
	LastTestAt        *time.Time `json:"lastTestAt" orm:"last_test_at"`
	UpdatedAt         time.Time  `json:"updatedAt" orm:"updated_at"`
}

type PublicModelView struct {
	Id               uint64   `json:"id" orm:"id"`
	PublicName       string   `json:"publicName" orm:"public_name"`
	InputPrice       *float64 `json:"inputPrice" orm:"input_price"`
	CachedInputPrice *float64 `json:"cachedInputPrice" orm:"cached_input_price"`
	CacheWritePrice  *float64 `json:"cacheWritePrice" orm:"cache_write_price"`
	OutputPrice      *float64 `json:"outputPrice" orm:"output_price"`
	ImageInputPrice  *float64 `json:"imageInputPrice" orm:"image_input_price"`
	AudioInputPrice  *float64 `json:"audioInputPrice" orm:"audio_input_price"`
	AudioOutputPrice *float64 `json:"audioOutputPrice" orm:"audio_output_price"`
	RequestPrice     *float64 `json:"requestPrice" orm:"request_price"`
	BillingMode      string   `json:"billingMode" orm:"billing_mode"`
}

type DiscoveredModel struct {
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
}

func New(appSvc *app.Service, typeSvc *channeltype.Service, groupSvc *channelgroup.Service, resilienceSvc *system.Service) *Service {
	return &Service{app: appSvc, types: typeSvc, groups: groupSvc, resilience: resilienceSvc}
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	var rows []entity.Channels
	if err := dao.Channels.Ctx(ctx).OrderDesc(dao.Channels.Columns().Priority).OrderDesc(dao.Channels.Columns().Id).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list channels")
	}
	views := make([]View, 0, len(rows))
	types, err := s.types.List(ctx)
	if err != nil {
		return nil, err
	}
	typeByCode := make(map[string]channeltype.View, len(types))
	for _, item := range types {
		typeByCode[item.Code] = item
	}
	for i := range rows {
		view := s.toView(rows[i])
		if item, ok := typeByCode[rows[i].Type]; ok {
			view.TypeName = item.Name
			view.CostQueryMode = item.Config.Costs.Adapter
		} else {
			view.TypeName = rows[i].Type
		}
		view.DiscoveredModels, _ = dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().ChannelId, rows[i].Id).Count()
		view.EnabledModelCount, _ = dao.ChannelModels.Ctx(ctx).
			Where(do.ChannelModels{ChannelId: rows[i].Id, Enabled: 1}).Count()
		view.GroupIDs, err = s.groups.ChannelIDs(ctx, rows[i].Id)
		if err != nil {
			return nil, err
		}
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
	typeRow, typeConfig, err := s.writableType(ctx, input.Type)
	if err != nil {
		return 0, err
	}
	data := do.Channels{
		Name:            strings.TrimSpace(input.Name),
		Type:            typeRow.Code,
		BaseUrl:         baseURL,
		ApiKeyCipher:    apiKeyCipher,
		OrganizationId:  strings.TrimSpace(input.OrganizationID),
		ProjectId:       strings.TrimSpace(input.ProjectID),
		Status:          boolStatus(input.Status),
		Priority:        input.Priority,
		Weight:          normalizeWeight(input.Weight),
		CostQueryMode:   typeConfig.Costs.Adapter,
		CostQueryConfig: "{}",
	}
	if input.ManagementKey != nil && strings.TrimSpace(*input.ManagementKey) != "" {
		data.ManagementKeyCipher, err = s.app.Secrets.Encrypt(strings.TrimSpace(*input.ManagementKey))
		if err != nil {
			return 0, err
		}
	}
	var id uint64
	err = dao.Channels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		created, createErr := dao.Channels.Ctx(txCtx).Data(data).InsertAndGetId()
		if createErr != nil {
			return gerror.Wrap(createErr, "create channel")
		}
		id = uint64(created)
		return s.groups.SetChannelIDs(txCtx, id, input.GroupIDs)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
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
	typeRow, typeConfig, err := s.writableType(ctx, input.Type)
	if err != nil {
		return err
	}
	data := do.Channels{
		Name:                   strings.TrimSpace(input.Name),
		Type:                   typeRow.Code,
		BaseUrl:                baseURL,
		OrganizationId:         strings.TrimSpace(input.OrganizationID),
		ProjectId:              strings.TrimSpace(input.ProjectID),
		Status:                 boolStatus(input.Status),
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		Priority:               input.Priority,
		Weight:                 normalizeWeight(input.Weight),
		CostQueryMode:          typeConfig.Costs.Adapter,
		CostQueryConfig:        "{}",
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
	if err = dao.Channels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, updateErr := dao.Channels.Ctx(txCtx).Where(dao.Channels.Columns().Id, current.Id).Data(data).Update(); updateErr != nil {
			return gerror.Wrap(updateErr, "update channel")
		}
		return s.groups.SetChannelIDs(txCtx, current.Id, input.GroupIDs)
	}); err != nil {
		return err
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

func (s *Service) DiscoverModels(ctx context.Context, channelID uint64) ([]DiscoveredModel, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return nil, err
	}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return nil, err
	}
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Models.Path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, config.Models.Method, endpoint, nil)
	if err != nil {
		return nil, gerror.Wrap(err, "create model discovery request")
	}
	if err = s.setConfiguredHeaders(ctx, req, channel, config.Models.AuthType, config.Models.HeaderName, config.Models.HeaderPrefix); err != nil {
		return nil, err
	}
	resp, err := s.app.HTTP.Do(req)
	if err != nil {
		return nil, gerror.Wrap(err, "fetch upstream models")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, upstreamModelQueryError(resp.StatusCode, body)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, gerror.Wrap(err, "read upstream models")
	}
	if !gjson.ValidBytes(body) {
		return nil, gerror.New("upstream model query returned invalid JSON")
	}
	var existing []entity.ChannelModels
	if err = dao.ChannelModels.Ctx(ctx).
		Where(do.ChannelModels{ChannelId: channelID, Enabled: 1}).
		Scan(&existing); err != nil {
		return nil, gerror.Wrap(err, "load selected models")
	}
	selected := make(map[string]struct{}, len(existing))
	for _, model := range existing {
		selected[model.UpstreamName] = struct{}{}
	}

	names, err := modelNamesFromJSON(body, config.Models.ListPath, config.Models.IDPath)
	if err != nil {
		return nil, err
	}
	names = normalizeModelNames(names)
	models := make([]DiscoveredModel, 0, len(names))
	for _, name := range names {
		_, isSelected := selected[name]
		models = append(models, DiscoveredModel{Name: name, Selected: isSelected})
	}
	return models, nil
}

func (s *Service) SelectModels(ctx context.Context, channelID uint64, input adminapi.ModelSelectionInput) ([]ModelView, error) {
	if _, err := s.Get(ctx, channelID); err != nil {
		return nil, err
	}
	names := normalizeModelNames(input.ModelNames)
	if len(names) > 2000 {
		return nil, gerror.New("too many models selected")
	}
	for _, name := range names {
		if len(name) > 191 {
			return nil, gerror.Newf("model name is too long: %s", name)
		}
	}
	selected := make(map[string]struct{}, len(names))
	for _, name := range names {
		selected[name] = struct{}{}
	}

	err := dao.ChannelModels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		var existing []entity.ChannelModels
		if scanErr := dao.ChannelModels.Ctx(txCtx).
			Where(dao.ChannelModels.Columns().ChannelId, channelID).
			Scan(&existing); scanErr != nil {
			return gerror.Wrap(scanErr, "load channel models")
		}
		for _, model := range existing {
			_, enabled := selected[model.UpstreamName]
			delete(selected, model.UpstreamName)
			if model.Enabled == boolInt(enabled) {
				continue
			}
			if _, updateErr := dao.ChannelModels.Ctx(txCtx).
				Where(dao.ChannelModels.Columns().Id, model.Id).
				Data(do.ChannelModels{Enabled: boolInt(enabled)}).
				Update(); updateErr != nil {
				return gerror.Wrap(updateErr, "update model selection")
			}
		}
		for _, name := range names {
			if _, missing := selected[name]; !missing {
				continue
			}
			if _, insertErr := dao.ChannelModels.Ctx(txCtx).Data(do.ChannelModels{
				ChannelId:    channelID,
				PublicName:   name,
				UpstreamName: name,
				Discovered:   1,
				Enabled:      1,
			}).Insert(); insertErr != nil {
				return gerror.Wrap(insertErr, "save selected model")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = s.invalidateRoutes(ctx); err != nil {
		return nil, err
	}
	return s.ListModels(ctx, channelID)
}

func (s *Service) ListModels(ctx context.Context, channelID uint64) ([]ModelView, error) {
	rows := make([]ModelView, 0)
	model := dao.ChannelModels.Ctx(ctx).As("m").
		Fields(`m.id,m.channel_id,c.name AS channel_name,m.public_name,m.upstream_name,m.discovered,m.enabled,
			p.input_price,p.cached_input_price,p.cache_write_price,p.output_price,p.image_input_price,p.audio_input_price,p.audio_output_price,p.request_price,
			COALESCE(p.billing_mode,'token') AS billing_mode,m.last_test_endpoint,m.last_test_status,
			m.last_test_latency_ms,m.last_test_error,m.last_test_at,m.updated_at`).
		LeftJoin(dao.Channels.Table()+" c", "c.id=m.channel_id").
		LeftJoin(dao.ModelPrices.Table()+" p", "p.public_name=m.public_name AND p.deleted_at IS NULL")
	if channelID > 0 {
		model = model.Where("m.channel_id", channelID)
	}
	err := model.OrderAsc("m.public_name").OrderAsc("m.upstream_name").Scan(&rows)
	return rows, gerror.Wrap(err, "list channel models")
}

func (s *Service) ListPublicModels(ctx context.Context) ([]PublicModelView, error) {
	rows := make([]PublicModelView, 0)
	err := dao.ChannelModels.Ctx(ctx).As("m").
		Fields(`MIN(m.id) AS id,m.public_name,p.input_price,p.cached_input_price,p.cache_write_price,p.output_price,p.image_input_price,p.audio_input_price,p.audio_output_price,p.request_price,COALESCE(p.billing_mode,'token') AS billing_mode`).
		LeftJoin(dao.ModelPrices.Table()+" p", "p.public_name=m.public_name AND p.deleted_at IS NULL").
		Group("m.public_name,p.input_price,p.cached_input_price,p.cache_write_price,p.output_price,p.image_input_price,p.audio_input_price,p.audio_output_price,p.request_price,p.billing_mode").
		OrderAsc("m.public_name").
		Scan(&rows)
	return rows, gerror.Wrap(err, "list public models")
}

func normalizeModelNames(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i]), strings.ToLower(result[j])
		if left == right {
			return result[i] < result[j]
		}
		return left < right
	})
	return result
}

func modelNamesFromJSON(body []byte, listPath, idPath string) ([]string, error) {
	items := gjson.ParseBytes(body)
	if listPath != "" {
		items = gjson.GetBytes(body, listPath)
	}
	if !items.IsArray() {
		return nil, gerror.New("model list path did not resolve to an array")
	}
	names := make([]string, 0, len(items.Array()))
	for _, item := range items.Array() {
		name := strings.TrimSpace(item.Get(idPath).String())
		if name != "" {
			names = append(names, name)
		}
	}
	return normalizeModelNames(names), nil
}

func upstreamModelQueryError(status int, body []byte) error {
	if status != http.StatusTooManyRequests {
		return gerror.Newf("upstream model query returned HTTP %d", status)
	}

	var (
		code    = strings.ToUpper(strings.TrimSpace(firstJSONText(body, "code", "error.code")))
		message = strings.ToUpper(strings.TrimSpace(firstJSONText(body, "message", "error.message")))
	)
	if strings.Contains(code, "DAILY_LIMIT_EXCEEDED") ||
		(strings.Contains(code, "USAGE_LIMIT_EXCEEDED") &&
			(strings.Contains(message, "DAILY_LIMIT_EXCEEDED") || strings.Contains(message, "DAILY USAGE LIMIT"))) {
		return gerror.New("上游每日用量额度已用尽，请在上游补充额度或等待每日额度重置")
	}
	return gerror.New("上游请求受限（HTTP 429），请稍后重试或检查上游额度")
}

func firstJSONText(body []byte, paths ...string) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func (s *Service) UpdateModel(ctx context.Context, id uint64, input adminapi.ModelInput) error {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, id).Scan(&model); err != nil {
		return gerror.Wrap(err, "find model")
	}
	if model.Id == 0 {
		return gerror.New("model not found")
	}
	publicName := strings.TrimSpace(input.PublicName)
	modelData := do.ChannelModels{
		PublicName:   strings.TrimSpace(input.PublicName),
		UpstreamName: strings.TrimSpace(input.UpstreamName),
		Enabled:      boolInt(input.Enabled),
	}
	err := dao.ChannelModels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, updateErr := dao.ChannelModels.Ctx(txCtx).Where(dao.ChannelModels.Columns().Id, id).Data(modelData).Update(); updateErr != nil {
			return gerror.Wrap(updateErr, "update channel model")
		}
		return s.replacePublicPrice(txCtx, publicName, modelPriceValues{
			Input:       input.InputPrice,
			CachedInput: input.CachedInputPrice,
			CacheWrite:  input.CacheWritePrice,
			Output:      input.OutputPrice,
			ImageInput:  input.ImageInputPrice,
			AudioInput:  input.AudioInputPrice,
			AudioOutput: input.AudioOutputPrice,
			Request:     input.RequestPrice,
		})
	})
	if err != nil {
		return err
	}
	return s.invalidateRoutes(ctx)
}

func (s *Service) UpdatePublicModelPrice(ctx context.Context, id uint64, input adminapi.ModelPriceInput) error {
	modelName, err := s.publicModelName(ctx, id)
	if err != nil {
		return err
	}
	return s.updatePublicModelPrice(ctx, modelName, input)
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
	if !row.AutoDisabledAt.IsZero() {
		value := row.AutoDisabledAt
		view.AutoDisabled = true
		view.AutoDisabledAt = &value
		view.AutoDisabledReason = row.AutoDisabledReason
		if row.AutoDisabledStatusCode > 0 {
			statusCode := row.AutoDisabledStatusCode
			view.AutoDisabledStatusCode = &statusCode
		}
	}
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
	return value, nil
}

func (s *Service) writableType(ctx context.Context, code string) (entity.ChannelTypes, channeltype.Config, error) {
	row, config, err := s.types.GetByCode(ctx, code)
	if err != nil {
		return row, config, err
	}
	if row.Status != 1 {
		return row, config, gerror.New("channel type is disabled")
	}
	return row, config, nil
}

func (s *Service) setConfiguredHeaders(ctx context.Context, req *http.Request, channel entity.Channels, authType, headerName, headerPrefix string) error {
	req.Header.Set("Accept", "application/json")
	switch authType {
	case channeltype.AuthNone:
	case channeltype.AuthChannelKey:
		key, err := s.app.Secrets.Decrypt(channel.ApiKeyCipher)
		if err != nil {
			return err
		}
		req.Header.Set(headerName, headerPrefix+key)
	case channeltype.AuthManagementKey:
		if channel.ManagementKeyCipher == "" {
			return gerror.New("channel type requires a management key")
		}
		key, err := s.app.Secrets.Decrypt(channel.ManagementKeyCipher)
		if err != nil {
			return err
		}
		req.Header.Set(headerName, headerPrefix+key)
	default:
		return gerror.New("unsupported channel type auth")
	}
	if channel.OrganizationId != "" {
		req.Header.Set("OpenAI-Organization", channel.OrganizationId)
	}
	if channel.ProjectId != "" {
		req.Header.Set("OpenAI-Project", channel.ProjectId)
	}
	return nil
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
