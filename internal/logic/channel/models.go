package channel

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) DiscoverModels(ctx context.Context, channelID uint64) ([]DiscoveredModel, error) {
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
	credential, err := s.CredentialForTest(ctx, channelID, 0)
	if err != nil && config.Models.AuthType != "management_key" && config.Models.AuthType != "none" {
		return nil, err
	}
	body, err := s.fetchUpstreamJSON(ctx, channel, credential.APIKeyCipher, upstreamJSONRequest{
		Method:       config.Models.Method,
		Endpoint:     endpoint,
		AuthType:     config.Models.AuthType,
		HeaderName:   config.Models.HeaderName,
		HeaderPrefix: config.Models.HeaderPrefix,
		BodyLimit:    4 << 20,
		RequestError: "create model discovery request",
		FetchError:   "fetch upstream models",
		ReadError:    "read upstream models",
		InvalidError: "upstream model query returned invalid JSON",
		StatusError:  upstreamModelQueryError,
	})
	if err != nil {
		return nil, err
	}
	var existing []entity.ChannelModels
	if err = dao.ChannelModels.Ctx(ctx).
		Where(do.ChannelModels{ChannelId: channelID}).
		Scan(&existing); err != nil {
		return nil, gerror.Wrap(err, "load channel models")
	}
	existingByUpstream := make(map[string]entity.ChannelModels, len(existing))
	for _, model := range existing {
		existingByUpstream[model.UpstreamName] = model
	}

	names, err := modelNamesFromJSON(body, config.Models.ListPath, config.Models.IDPath)
	if err != nil {
		return nil, err
	}
	models := make([]DiscoveredModel, 0, len(names))
	for _, name := range names {
		model, exists := existingByUpstream[name]
		models = append(models, DiscoveredModel{
			Name:       name,
			PublicName: stringOrDefault(model.PublicName, name),
			Selected:   exists && model.Enabled == 1,
		})
	}
	return models, nil
}

func (s *sChannel) SelectModels(ctx context.Context, channelID uint64, input adminapi.ModelSelectionInput) ([]ModelView, error) {
	if _, err := s.Get(ctx, channelID); err != nil {
		return nil, err
	}
	mappings, err := normalizeModelMappings(input)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		selected[mapping.UpstreamName] = mapping.PublicName
	}

	err = dao.ChannelModels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		var existing []entity.ChannelModels
		if scanErr := dao.ChannelModels.Ctx(txCtx).
			Where(dao.ChannelModels.Columns().ChannelId, channelID).
			Scan(&existing); scanErr != nil {
			return gerror.Wrap(scanErr, "load channel models")
		}
		for _, model := range existing {
			publicName, enabled := selected[model.UpstreamName]
			if enabled {
				delete(selected, model.UpstreamName)
			}
			data := do.ChannelModels{Enabled: boolInt(enabled)}
			if enabled && model.PublicName != publicName {
				data.PublicName = publicName
			}
			if model.Enabled == boolInt(enabled) && (!enabled || model.PublicName == publicName) {
				continue
			}
			if _, updateErr := dao.ChannelModels.Ctx(txCtx).
				Where(dao.ChannelModels.Columns().Id, model.Id).
				Data(data).
				Update(); updateErr != nil {
				return gerror.Wrap(updateErr, "update model selection")
			}
		}
		for upstreamName, publicName := range selected {
			if _, insertErr := dao.ChannelModels.Ctx(txCtx).Data(do.ChannelModels{
				ChannelId:    channelID,
				PublicName:   publicName,
				UpstreamName: upstreamName,
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
	s.InvalidateListCache(ctx)
	if err = s.invalidateRoutes(ctx); err != nil {
		return nil, err
	}
	return s.ListModels(ctx, channelID)
}

type modelMapping struct {
	UpstreamName string
	PublicName   string
}

func normalizeModelMappings(input adminapi.ModelSelectionInput) ([]modelMapping, error) {
	items := input.Models
	if len(items) == 0 {
		for _, name := range normalizeModelNames(input.ModelNames) {
			items = append(items, adminapi.ModelMappingInput{UpstreamName: name, PublicName: name})
		}
	}
	if len(items) > 2000 {
		return nil, gerror.New("too many models selected")
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]modelMapping, 0, len(items))
	for _, item := range items {
		upstreamName := strings.TrimSpace(item.UpstreamName)
		if upstreamName == "" {
			continue
		}
		if len(upstreamName) > 191 {
			return nil, gerror.Newf("upstream model name is too long: %s", upstreamName)
		}
		if _, exists := seen[upstreamName]; exists {
			return nil, gerror.Newf("duplicate upstream model: %s", upstreamName)
		}
		seen[upstreamName] = struct{}{}
		publicName := strings.TrimSpace(item.PublicName)
		if publicName == "" {
			publicName = upstreamName
		}
		if len(publicName) > 191 {
			return nil, gerror.Newf("public model name is too long: %s", publicName)
		}
		result = append(result, modelMapping{UpstreamName: upstreamName, PublicName: publicName})
	}
	return result, nil
}

func stringOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (s *sChannel) ListModels(ctx context.Context, channelID uint64) ([]ModelView, error) {
	return s.listModelViews(ctx, channelID)
}

func (s *sChannel) DeleteFailedModels(ctx context.Context, channelID uint64) (int, error) {
	if _, err := s.Get(ctx, channelID); err != nil {
		return 0, err
	}
	result, err := dao.ChannelModels.Ctx(ctx).Where(do.ChannelModels{
		ChannelId:      channelID,
		Enabled:        1,
		LastTestStatus: "failed",
	}).Delete()
	if err != nil {
		return 0, gerror.Wrap(err, "delete failed channel models")
	}
	s.InvalidateListCache(ctx)
	if err = s.invalidateRoutes(ctx); err != nil {
		return 0, err
	}
	deleted, _ := result.RowsAffected()
	return int(deleted), nil
}

func (s *sChannel) ListPublicModels(ctx context.Context) ([]PublicModelView, error) {
	return s.listPublicModelViews(ctx)
}

func (s *sChannel) UpdateModel(ctx context.Context, id uint64, input adminapi.ModelInput) error {
	var model entity.ChannelModels
	if err := dao.ChannelModels.Ctx(ctx).Where(dao.ChannelModels.Columns().Id, id).Scan(&model); err != nil {
		return gerror.Wrap(err, "find model")
	}
	if model.Id == 0 {
		return gerror.New("model not found")
	}
	publicName := strings.TrimSpace(input.PublicName)
	modelData := do.ChannelModels{
		PublicName:   publicName,
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
	s.InvalidateListCache(ctx)
	if err = s.invalidateRoutes(ctx); err != nil {
		return err
	}
	return s.prices.Load(ctx)
}

func (s *sChannel) UpdatePublicModelPrice(ctx context.Context, id uint64, input adminapi.ModelPriceInput) error {
	modelName, err := s.publicModelName(ctx, id)
	if err != nil {
		return err
	}
	if err = s.updatePublicModelPrice(ctx, modelName, input); err != nil {
		return err
	}
	return s.prices.Load(ctx)
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
