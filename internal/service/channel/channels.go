package channel

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

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
	if input.HealthCheckModelID != 0 {
		return 0, gerror.New("test model can only be selected after the channel is created")
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
	advancedConfig, err := advancedConfigJSON(input.AdvancedConfig, "")
	if err != nil {
		return 0, err
	}
	data := do.Channels{
		Name:               strings.TrimSpace(input.Name),
		Type:               typeRow.Code,
		BaseUrl:            baseURL,
		ApiKeyCipher:       apiKeyCipher,
		OrganizationId:     strings.TrimSpace(input.OrganizationID),
		ProjectId:          strings.TrimSpace(input.ProjectID),
		Status:             boolStatus(input.Status),
		Priority:           input.Priority,
		Weight:             normalizeWeight(input.Weight),
		AutoDisableEnabled: boolInt(channelAutoDisableEnabled(input.AutoDisableEnabled, true)),
		CostQueryMode:      typeConfig.Costs.Adapter,
		CostQueryConfig:    "{}",
		AdvancedConfig:     advancedConfig,
	}
	if input.ManagementKey != nil && strings.TrimSpace(*input.ManagementKey) != "" {
		data.ManagementKeyCipher, err = s.app.Secrets.Encrypt(strings.TrimSpace(*input.ManagementKey))
		if err != nil {
			return 0, err
		}
	}
	if input.ProxyURL != nil && strings.TrimSpace(*input.ProxyURL) != "" {
		data.ProxyUrlCipher, err = s.encryptProxyURL(*input.ProxyURL)
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
	advancedConfig, err := advancedConfigJSON(input.AdvancedConfig, current.AdvancedConfig)
	if err != nil {
		return err
	}
	healthCheckModelID, err := s.validateHealthCheckModel(ctx, current.Id, input.HealthCheckModelID)
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
		AutoDisabledSource:     gdb.Raw("NULL"),
		Priority:               input.Priority,
		Weight:                 normalizeWeight(input.Weight),
		HealthCheckModelId:     healthCheckModelID,
		AutoDisableEnabled:     boolInt(channelAutoDisableEnabled(input.AutoDisableEnabled, current.AutoDisableEnabled == 1)),
		CostQueryMode:          typeConfig.Costs.Adapter,
		CostQueryConfig:        "{}",
		AdvancedConfig:         advancedConfig,
	}
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		data.ApiKeyCipher, err = s.app.Secrets.Encrypt(strings.TrimSpace(*input.APIKey))
		if err != nil {
			return err
		}
	}
	if input.ProxyURL != nil {
		if strings.TrimSpace(*input.ProxyURL) == "" {
			data.ProxyUrlCipher = gdb.Raw("NULL")
		} else {
			data.ProxyUrlCipher, err = s.encryptProxyURL(*input.ProxyURL)
			if err != nil {
				return err
			}
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

func (s *Service) toView(row entity.Channels) View {
	advancedConfig, err := ParseAdvancedConfig([]byte(row.AdvancedConfig))
	if err != nil {
		advancedConfig = DefaultAdvancedConfig()
	}
	view := View{
		Id:                 row.Id,
		Name:               row.Name,
		Type:               row.Type,
		BaseURL:            row.BaseUrl,
		HasAPIKey:          row.ApiKeyCipher != "",
		HasManagementKey:   row.ManagementKeyCipher != "",
		HasProxy:           row.ProxyUrlCipher != "",
		OrganizationID:     row.OrganizationId,
		ProjectID:          row.ProjectId,
		Status:             row.Status,
		Priority:           row.Priority,
		Weight:             row.Weight,
		HealthCheckModelID: row.HealthCheckModelId,
		AutoDisableEnabled: row.AutoDisableEnabled == 1,
		CostQueryMode:      row.CostQueryMode,
		AdvancedConfig:     advancedConfig,
		LastTestStatus:     row.LastTestStatus,
		LastTestLatencyMs:  row.LastTestLatencyMs,
		LastTestError:      row.LastTestError,
		LastCostCurrency:   row.LastCostCurrency,
		CreatedAt:          row.CreatedAt,
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

func advancedConfigJSON(raw []byte, fallback string) (string, error) {
	if len(raw) == 0 && fallback != "" {
		return fallback, nil
	}
	config, err := ParseAdvancedConfig(raw)
	if err != nil {
		return "", err
	}
	return MarshalAdvancedConfig(config)
}
