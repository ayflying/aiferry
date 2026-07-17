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
	body, err := s.fetchUpstreamJSON(ctx, channel, upstreamJSONRequest{
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

func (s *Service) DeleteFailedModels(ctx context.Context, channelID uint64) (int, error) {
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
	if err = s.invalidateRoutes(ctx); err != nil {
		return 0, err
	}
	deleted, _ := result.RowsAffected()
	return int(deleted), nil
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
	if err = s.invalidateRoutes(ctx); err != nil {
		return err
	}
	return s.prices.Load(ctx)
}

func (s *Service) UpdatePublicModelPrice(ctx context.Context, id uint64, input adminapi.ModelPriceInput) error {
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
