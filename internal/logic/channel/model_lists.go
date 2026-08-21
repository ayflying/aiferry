package channel

import (
	"context"
	"sort"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type modelPriceView struct {
	PublicName       string   `orm:"public_name"`
	BillingMode      string   `orm:"billing_mode"`
	InputPrice       *float64 `orm:"input_price"`
	CachedInputPrice *float64 `orm:"cached_input_price"`
	CacheWritePrice  *float64 `orm:"cache_write_price"`
	OutputPrice      *float64 `orm:"output_price"`
	ImageInputPrice  *float64 `orm:"image_input_price"`
	AudioInputPrice  *float64 `orm:"audio_input_price"`
	AudioOutputPrice *float64 `orm:"audio_output_price"`
	RequestPrice     *float64 `orm:"request_price"`
}

func (s *sChannel) listModelViews(ctx context.Context, channelID uint64) ([]ModelView, error) {
	columns := dao.ChannelModels.Columns()
	query := dao.ChannelModels.Ctx(ctx)
	if channelID > 0 {
		query = query.Where(columns.ChannelId, channelID)
	}
	models := make([]entity.ChannelModels, 0)
	if err := query.OrderAsc(columns.PublicName).OrderAsc(columns.UpstreamName).Scan(&models); err != nil {
		return nil, gerror.Wrap(err, "load channel models")
	}
	channelNames, err := loadModelChannelNames(ctx, modelChannelIDs(models))
	if err != nil {
		return nil, err
	}
	prices, err := loadModelPrices(ctx, modelPublicNames(models))
	if err != nil {
		return nil, err
	}
	result := make([]ModelView, 0, len(models))
	for _, model := range models {
		result = append(result, modelViewFromEntity(model, channelNames[model.ChannelId], prices[model.PublicName]))
	}
	return result, nil
}

func (s *sChannel) listPublicModelViews(ctx context.Context) ([]PublicModelView, error) {
	columns := dao.ChannelModels.Columns()
	models := make([]entity.ChannelModels, 0)
	if err := dao.ChannelModels.Ctx(ctx).
		Fields(columns.Id, columns.PublicName).
		Where(columns.Enabled, 1).
		OrderAsc(columns.Id).
		Scan(&models); err != nil {
		return nil, gerror.Wrap(err, "load public channel models")
	}
	firstByName := make(map[string]uint64)
	for _, model := range models {
		if _, exists := firstByName[model.PublicName]; !exists {
			firstByName[model.PublicName] = model.Id
		}
	}
	names := make([]string, 0, len(firstByName))
	for name := range firstByName {
		names = append(names, name)
	}
	prices, err := loadModelPrices(ctx, names)
	if err != nil {
		return nil, err
	}
	result := make([]PublicModelView, 0, len(names))
	for _, name := range names {
		price := prices[name]
		result = append(result, PublicModelView{
			Id:               firstByName[name],
			PublicName:       name,
			InputPrice:       price.InputPrice,
			CachedInputPrice: price.CachedInputPrice,
			CacheWritePrice:  price.CacheWritePrice,
			OutputPrice:      price.OutputPrice,
			ImageInputPrice:  price.ImageInputPrice,
			AudioInputPrice:  price.AudioInputPrice,
			AudioOutputPrice: price.AudioOutputPrice,
			RequestPrice:     price.RequestPrice,
			BillingMode:      modelBillingMode(price),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PublicName < result[j].PublicName })
	return result, nil
}

func loadModelChannelNames(ctx context.Context, channelIDs []uint64) (map[uint64]string, error) {
	result := make(map[uint64]string)
	if len(channelIDs) == 0 {
		return result, nil
	}
	columns := dao.Channels.Columns()
	channels := make([]entity.Channels, 0, len(channelIDs))
	if err := dao.Channels.Ctx(ctx).
		Fields(columns.Id, columns.Name).
		WhereIn(columns.Id, channelIDs).
		Scan(&channels); err != nil {
		return nil, gerror.Wrap(err, "load model channels")
	}
	for _, channel := range channels {
		result[channel.Id] = channel.Name
	}
	return result, nil
}

func loadModelPrices(ctx context.Context, names []string) (map[string]modelPriceView, error) {
	result := make(map[string]modelPriceView)
	if len(names) == 0 {
		return result, nil
	}
	columns := dao.ModelPrices.Columns()
	rows := make([]modelPriceView, 0, len(names))
	if err := dao.ModelPrices.Ctx(ctx).
		Fields(columns.PublicName, columns.BillingMode, columns.InputPrice, columns.CachedInputPrice, columns.CacheWritePrice, columns.OutputPrice, columns.ImageInputPrice, columns.AudioInputPrice, columns.AudioOutputPrice, columns.RequestPrice).
		WhereIn(columns.PublicName, names).
		Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "load model prices")
	}
	for _, row := range rows {
		result[row.PublicName] = row
	}
	return result, nil
}

func modelViewFromEntity(model entity.ChannelModels, channelName string, price modelPriceView) ModelView {
	view := ModelView{
		Id:                model.Id,
		ChannelId:         model.ChannelId,
		ChannelName:       channelName,
		PublicName:        model.PublicName,
		UpstreamName:      model.UpstreamName,
		Discovered:        model.Discovered,
		Enabled:           model.Enabled,
		InputPrice:        price.InputPrice,
		CachedInputPrice:  price.CachedInputPrice,
		CacheWritePrice:   price.CacheWritePrice,
		OutputPrice:       price.OutputPrice,
		ImageInputPrice:   price.ImageInputPrice,
		AudioInputPrice:   price.AudioInputPrice,
		AudioOutputPrice:  price.AudioOutputPrice,
		RequestPrice:      price.RequestPrice,
		BillingMode:       modelBillingMode(price),
		LastTestEndpoint:  model.LastTestEndpoint,
		LastTestStatus:    model.LastTestStatus,
		LastTestLatencyMs: model.LastTestLatencyMs,
		LastTestError:     model.LastTestError,
		UpdatedAt:         model.UpdatedAt,
	}
	if !model.LastTestAt.IsZero() {
		lastTestAt := model.LastTestAt
		view.LastTestAt = &lastTestAt
	}
	return view
}

func modelBillingMode(price modelPriceView) string {
	if price.BillingMode == "" {
		return "token"
	}
	return price.BillingMode
}

func modelChannelIDs(models []entity.ChannelModels) []uint64 {
	ids := make(map[uint64]struct{})
	for _, model := range models {
		ids[model.ChannelId] = struct{}{}
	}
	return sortedModelIDs(ids)
}

func modelPublicNames(models []entity.ChannelModels) []string {
	names := make(map[string]struct{})
	for _, model := range models {
		names[model.PublicName] = struct{}{}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func sortedModelIDs(values map[uint64]struct{}) []uint64 {
	result := make([]uint64, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
