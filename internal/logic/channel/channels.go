package channel

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) List(ctx context.Context) ([]View, error) {
	if cached, ok := s.readListCache(ctx); ok {
		return cached, nil
	}
	views, err := s.listFromDatabase(ctx)
	if err != nil {
		return nil, err
	}
	s.writeListCache(ctx, views)
	return views, nil
}

func (s *sChannel) listFromDatabase(ctx context.Context) ([]View, error) {
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
		usageQuery := false
		var costConfig channeltype.CostConfig
		if item, ok := typeByCode[rows[i].Type]; ok {
			view.TypeName = item.Name
			view.CostQueryMode = item.Config.Costs.Adapter
			view.CostQueryType = item.Config.Costs.ValueType
			costConfig = item.Config.Costs
			usageQuery = channeltype.IsUsageCost(item.Config.Costs)
		} else {
			view.TypeName = rows[i].Type
		}
		view.DiscoveredModels, _ = dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().ChannelId, rows[i].Id).Count()
		view.EnabledModelCount, _ = dao.ChannelModels.Ctx(ctx).
			Where(do.ChannelModels{ChannelId: rows[i].Id, Enabled: 1}).Count()
		view.CredentialCount, _ = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: rows[i].Id}).Count()
		view.ActiveCredentialCount, _ = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: rows[i].Id, Status: 1}).Count()
		view.HasAPIKey = view.CredentialCount > 0
		view.CredentialsUnavailable = view.CredentialCount == 0 || view.ActiveCredentialCount == 0
		view.CostSummaries, err = s.channelCostSummaries(ctx, rows[i].Id, usageQuery)
		if err != nil {
			return nil, err
		}
		applyUsageSummaryMetadata(view.CostSummaries, costConfig)
		currentCost, costErr := s.currentChannelCost(ctx, rows[i].Id)
		if costErr != nil {
			return nil, costErr
		}
		view.LastCostUsed = currentCost.Used
		view.LastCostRemaining = currentCost.Remaining
		view.LastCostCurrency = currentCost.Currency
		view.LastCostAt = currentCost.At
		if len(view.CostSummaries) == 0 && currentCost.Currency != "" {
			summary := CostSummary{Currency: currentCost.Currency, UsedAmount: currentCost.Used, RemainingAmount: currentCost.Remaining}
			if usageQuery {
				summary.Usage = currentCost.Used
			}
			view.CostSummaries = []CostSummary{summary}
			applyUsageSummaryMetadata(view.CostSummaries, costConfig)
		}
		view.GroupIDs, err = s.groups.ChannelIDs(ctx, rows[i].Id)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *sChannel) Get(ctx context.Context, id uint64) (entity.Channels, error) {
	var row entity.Channels
	if err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, id).Scan(&row); err != nil {
		return row, gerror.Wrap(err, "find channel")
	}
	if row.Id == 0 {
		return row, gerror.New("channel not found")
	}
	return row, nil
}

func (s *sChannel) Create(ctx context.Context, input adminapi.ChannelInput) (uint64, error) {
	if input.HealthCheckModelID != 0 {
		return 0, gerror.New("test model can only be selected after the channel is created")
	}
	baseURL, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return 0, err
	}
	apiKey := ""
	if input.APIKey != nil {
		apiKey = strings.TrimSpace(*input.APIKey)
	}
	apiKeyCipher, err := s.app.Secrets.Encrypt(apiKey)
	if err != nil {
		return 0, err
	}
	typeRow, typeConfig, err := s.writableType(ctx, input.Type)
	if err != nil {
		return 0, err
	}
	advancedConfig, err := advancedConfigJSON(input.AdvancedConfig, "", baseURL)
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
		if groupErr := s.groups.SetChannelIDs(txCtx, id, input.GroupIDs); groupErr != nil {
			return groupErr
		}
		return s.createCredentialTx(txCtx, id, apiKey)
	})
	if err != nil {
		return 0, err
	}
	s.InvalidateListCache(ctx)
	return id, nil
}

func (s *sChannel) Update(ctx context.Context, id uint64, input adminapi.ChannelInput) error {
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
	advancedConfig, err := advancedConfigJSON(input.AdvancedConfig, current.AdvancedConfig, baseURL)
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
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		if _, err = s.CreateCredential(ctx, current.Id, adminapi.ChannelCredentialInput{APIKey: strings.TrimSpace(*input.APIKey)}); err != nil {
			return err
		}
	}
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}

func (s *sChannel) Delete(ctx context.Context, id uint64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, id).Data(do.Channels{Status: 0}).Update(); err != nil {
		return gerror.Wrap(err, "disable channel before delete")
	}
	if _, err := dao.Channels.Ctx(ctx).Where(dao.Channels.Columns().Id, id).Delete(); err != nil {
		return gerror.Wrap(err, "delete channel")
	}
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}

func (s *sChannel) toView(row entity.Channels) View {
	advancedConfig, err := ParseAdvancedConfig([]byte(row.AdvancedConfig))
	if err != nil {
		advancedConfig = DefaultAdvancedConfig()
	}
	view := View{
		Id:                 row.Id,
		Name:               row.Name,
		Type:               row.Type,
		BaseURL:            row.BaseUrl,
		HasAPIKey:          false,
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
	return view
}

func advancedConfigJSON(raw []byte, fallback, primaryBaseURL string) (string, error) {
	if len(raw) == 0 && fallback != "" {
		raw = []byte(fallback)
	}
	config, err := ParseAdvancedConfig(raw)
	if err != nil {
		return "", err
	}
	backupBaseURLs, err := normalizeBackupBaseURLs(config.BackupBaseURLs, primaryBaseURL)
	if err != nil {
		return "", err
	}
	config.BackupBaseURLs = backupBaseURLs
	return MarshalAdvancedConfig(config)
}

func normalizeBackupBaseURLs(values []string, primaryBaseURL string) ([]string, error) {
	primaryBaseURL, err := normalizeBaseURL(primaryBaseURL)
	if err != nil {
		return nil, err
	}
	if len(values) > 8 {
		return nil, gerror.New("最多可配置 8 个备用 API 根地址")
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{primaryBaseURL: {}}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		normalized, normalizeErr := normalizeBaseURL(value)
		if normalizeErr != nil {
			return nil, gerror.New("备用 API 根地址必须是绝对 HTTP(S) 地址")
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}
