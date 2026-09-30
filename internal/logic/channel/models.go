package channel

import (
	"context"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/timewindow"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func (s *sChannel) SelectModels(ctx context.Context, channelID uint64, input adminapi.ModelSelectionInput) ([]ModelView, error) {
	if err := s.ensureOwned(ctx, channelID); err != nil {
		return nil, err
	}
	mappings, err := normalizeModelMappings(input)
	if err != nil {
		return nil, err
	}
	closedWindows, err := normalizeModelClosedWindows(input)
	if err != nil {
		return nil, err
	}
	err = dao.ChannelModels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		var existing []entity.ChannelModels
		if scanErr := dao.ChannelModels.Ctx(txCtx).
			Where(dao.ChannelModels.Columns().ChannelId, channelID).
			Scan(&existing); scanErr != nil {
			return gerror.Wrap(scanErr, "load channel models")
		}
		existingByKey := make(map[modelMapping]entity.ChannelModels, len(existing))
		existingByUpstream := make(map[string][]entity.ChannelModels, len(existing))
		for _, model := range existing {
			existingByKey[modelMapping{UpstreamName: model.UpstreamName, PublicName: model.PublicName}] = model
			existingByUpstream[model.UpstreamName] = append(existingByUpstream[model.UpstreamName], model)
		}
		used := make(map[uint64]struct{}, len(mappings))
		for _, mapping := range mappings {
			model, exists := existingByKey[mapping]
			if !exists {
				for _, candidate := range existingByUpstream[mapping.UpstreamName] {
					if _, alreadyUsed := used[candidate.Id]; !alreadyUsed {
						model, exists = candidate, true
						break
					}
				}
			}
			if exists {
				used[model.Id] = struct{}{}
				data := do.ChannelModels{PublicName: mapping.PublicName, Enabled: 1}
				needsUpdate := model.Enabled != 1 || model.PublicName != mapping.PublicName
				// 时段变化同样要落库；未提交该字段时保持库里原值。
				if window, submitted := closedWindows[mapping]; submitted && model.ClosedWindowsJson != window {
					data.ClosedWindowsJson = window
					needsUpdate = true
				}
				if !needsUpdate {
					continue
				}
				if _, updateErr := dao.ChannelModels.Ctx(txCtx).
					Where(dao.ChannelModels.Columns().Id, model.Id).
					Data(data).
					Update(); updateErr != nil {
					return gerror.Wrap(updateErr, "update model selection")
				}
				continue
			}
			insert := do.ChannelModels{
				ChannelId:    channelID,
				PublicName:   mapping.PublicName,
				UpstreamName: mapping.UpstreamName,
				Discovered:   1,
				Enabled:      1,
			}
			if window, submitted := closedWindows[mapping]; submitted {
				insert.ClosedWindowsJson = window
			}
			if _, insertErr := dao.ChannelModels.Ctx(txCtx).Data(insert).Insert(); insertErr != nil {
				return gerror.Wrap(insertErr, "save selected model")
			}
		}
		for _, model := range existing {
			if _, enabled := used[model.Id]; enabled || model.Enabled == 0 {
				continue
			}
			if _, updateErr := dao.ChannelModels.Ctx(txCtx).
				Where(dao.ChannelModels.Columns().Id, model.Id).
				Data(do.ChannelModels{Enabled: 0}).
				Update(); updateErr != nil {
				return gerror.Wrap(updateErr, "disable unselected model")
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
	seen := make(map[modelMapping]struct{}, len(items))
	result := make([]modelMapping, 0, len(items))
	for _, item := range items {
		upstreamName := strings.TrimSpace(item.UpstreamName)
		if upstreamName == "" {
			continue
		}
		if len(upstreamName) > 191 {
			return nil, gerror.Newf("upstream model name is too long: %s", upstreamName)
		}
		publicName := strings.TrimSpace(item.PublicName)
		if publicName == "" {
			publicName = upstreamName
		}
		if len(publicName) > 191 {
			return nil, gerror.Newf("public model name is too long: %s", publicName)
		}
		mapping := modelMapping{UpstreamName: upstreamName, PublicName: publicName}
		if _, exists := seen[mapping]; exists {
			return nil, gerror.Newf("duplicate model mapping: %s -> %s", upstreamName, publicName)
		}
		seen[mapping] = struct{}{}
		result = append(result, mapping)
	}
	return result, nil
}

// normalizeModelClosedWindows 解析每条映射随请求带上的定时关闭时段列表。
// 返回值只包含「本次显式提交了该字段」的映射：字段缺省（nil）不进结果，
// 保存时保持库里原值；提交空数组得到空串，表示清除已配置的全部时段。
func normalizeModelClosedWindows(input adminapi.ModelSelectionInput) (map[modelMapping]string, error) {
	windows := make(map[modelMapping]string, len(input.Models))
	for _, item := range input.Models {
		if item.ClosedWindows == nil {
			continue
		}
		upstreamName := strings.TrimSpace(item.UpstreamName)
		if upstreamName == "" {
			continue
		}
		publicName := strings.TrimSpace(item.PublicName)
		if publicName == "" {
			publicName = upstreamName
		}
		key := modelMapping{UpstreamName: upstreamName, PublicName: publicName}
		if _, exists := windows[key]; exists {
			continue
		}
		converted := make([]timewindow.Window, 0, len(item.ClosedWindows))
		for _, window := range item.ClosedWindows {
			converted = append(converted, timewindow.Window{TZ: window.TZ, Weekdays: window.Weekdays, Ranges: window.Ranges})
		}
		normalized, err := timewindow.NormalizeWindows(converted)
		if err != nil {
			return nil, gerror.Wrapf(err, "模型 %s 的关闭时段无效", publicName)
		}
		windows[key] = normalized
	}
	return windows, nil
}

// modelClosedWindows 把库里存的时段 JSON 转成视图列表（任一命中即关闭）；
// 内容异常或为空时按「未配置」处理（fail-open，不因配置脏数据阻断该模型的正常转发）。
func modelClosedWindows(raw string) []timewindow.Window {
	windows, err := timewindow.ParseList(raw)
	if err != nil || len(windows) == 0 {
		return nil
	}
	return windows
}

func stringOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (s *sChannel) ListModels(ctx context.Context, channelID uint64) ([]ModelView, error) {
	if err := s.ensureOwned(ctx, channelID); err != nil {
		return nil, err
	}
	return s.listModelViews(ctx, channelID)
}

func (s *sChannel) DeleteFailedModels(ctx context.Context, channelID uint64) (int, error) {
	if err := s.ensureOwned(ctx, channelID); err != nil {
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
	// 手动重新启用模型时重置健康评分并清除禁用标记，给它一个全新的开始。
	if boolInt(input.Enabled) == 1 {
		modelData.HealthScore = system.ModelHealthInitialScore
		modelData.AutoDisabledAt = gdb.Raw("NULL")
		modelData.AutoDisabledReason = gdb.Raw("NULL")
		modelData.AutoDisabledSource = gdb.Raw("NULL")
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
