package channel

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

// 本文件负责「公共模型价格」（model_prices 一行 = 一个公开模型的价格）的写入：
// 手动设置、同步合并、计费模式切换。价格规则（model_price_rules）的增删改查与
// 同步流程分别见 pricing.go 与 price_rules_sync.go。

func (s *sChannel) replacePublicPrice(ctx context.Context, modelName string, values modelPriceValues) error {
	return s.savePublicPrice(ctx, modelName, values, true, false, BillingModeToken)
}

func isMissingPublicPriceError(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (s *sChannel) mergePublicPrice(ctx context.Context, modelName string, values modelPriceValues) error {
	var current PublicModelView
	if err := dao.ModelPrices.Ctx(ctx).
		Where(dao.ModelPrices.Columns().PublicName, modelName).
		Scan(&current); err != nil && !isMissingPublicPriceError(err) {
		return gerror.Wrap(err, "load current public model price")
	}
	values = missingPublicPriceValues(values, current)
	if !values.hasAny() {
		return nil
	}
	return s.savePublicPrice(ctx, modelName, values, false, false, "")
}

// missingPublicPriceValues 只保留还没填过的字段：手动维护的价格优先，同步不覆盖。
// 但「已填 0 价」不算填过——旧版本把上游的 p * 0 落成了 0 价字段，若继续把它当
// 已有价格，即使上游后来给出真实价格也永远填不进去，模型会一直按 0 计费。
func missingPublicPriceValues(values modelPriceValues, current PublicModelView) modelPriceValues {
	values.Input = keepUnset(values.Input, current.InputPrice)
	values.CachedInput = keepUnset(values.CachedInput, current.CachedInputPrice)
	values.CacheWrite = keepUnset(values.CacheWrite, current.CacheWritePrice)
	values.Output = keepUnset(values.Output, current.OutputPrice)
	values.ImageInput = keepUnset(values.ImageInput, current.ImageInputPrice)
	values.AudioInput = keepUnset(values.AudioInput, current.AudioInputPrice)
	values.AudioOutput = keepUnset(values.AudioOutput, current.AudioOutputPrice)
	values.Request = keepUnset(values.Request, current.RequestPrice)
	return values
}

// keepUnset 判断同步值是否还能写入：当前值已填且非 0 时保留现状（手动价优先）。
// 当前值恰好是 0 且同步值非 0 时允许覆盖——0 在这里几乎只可能来自旧版本的
// 「系数全为 0」错误落库，不覆盖等于把错误永久固化。
func keepUnset(synced, current *float64) *float64 {
	if current == nil || (synced != nil && *synced != 0 && *current == 0) {
		return synced
	}
	return nil
}

func (values modelPriceValues) hasAny() bool {
	return values.Input != nil ||
		values.CachedInput != nil ||
		values.CacheWrite != nil ||
		values.Output != nil ||
		values.ImageInput != nil ||
		values.AudioInput != nil ||
		values.AudioOutput != nil ||
		values.Request != nil
}

func (s *sChannel) savePublicPrice(ctx context.Context, modelName string, values modelPriceValues, replaceToken, replaceRequest bool, billingMode string) error {
	data := do.ModelPrices{PublicName: modelName}
	if billingMode != "" {
		data.BillingMode = billingMode
	}
	if replaceToken || values.Input != nil {
		data.InputPrice = nullableNumber(values.Input)
	}
	if replaceToken || values.CachedInput != nil {
		data.CachedInputPrice = nullableNumber(values.CachedInput)
	}
	if replaceToken || values.CacheWrite != nil {
		data.CacheWritePrice = nullableNumber(values.CacheWrite)
	}
	if replaceToken || values.Output != nil {
		data.OutputPrice = nullableNumber(values.Output)
	}
	if replaceToken || values.ImageInput != nil {
		data.ImageInputPrice = nullableNumber(values.ImageInput)
	}
	if replaceToken || values.AudioInput != nil {
		data.AudioInputPrice = nullableNumber(values.AudioInput)
	}
	if replaceToken || values.AudioOutput != nil {
		data.AudioOutputPrice = nullableNumber(values.AudioOutput)
	}
	if replaceRequest || values.Request != nil {
		data.RequestPrice = nullableNumber(values.Request)
	}
	result, err := dao.ModelPrices.Ctx(ctx).Where(dao.ModelPrices.Columns().PublicName, modelName).Data(data).Update()
	if err != nil {
		return gerror.Wrap(err, "update public model price")
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		return nil
	}
	if _, err = dao.ModelPrices.Ctx(ctx).Data(data).Insert(); err != nil {
		return gerror.Wrap(err, "create public model price")
	}
	return nil
}

func (s *sChannel) updatePublicModelPrice(ctx context.Context, modelName string, input adminapi.ModelPriceInput) error {
	mode, err := normalizeBillingMode(input.BillingMode)
	if err != nil {
		return err
	}
	values := modelPriceValues{
		Input:       input.InputPrice,
		CachedInput: input.CachedInputPrice,
		CacheWrite:  input.CacheWritePrice,
		Output:      input.OutputPrice,
		ImageInput:  input.ImageInputPrice,
		AudioInput:  input.AudioInputPrice,
		AudioOutput: input.AudioOutputPrice,
		Request:     input.RequestPrice,
	}
	switch mode {
	case BillingModeToken:
		return s.savePublicPrice(ctx, modelName, values, true, false, mode)
	case BillingModeRequest:
		return s.savePublicPrice(ctx, modelName, values, false, true, mode)
	default:
		return s.savePublicPrice(ctx, modelName, values, false, false, mode)
	}
}

func normalizeBillingMode(value string) (string, error) {
	switch value = strings.TrimSpace(value); value {
	case "", BillingModeToken:
		return BillingModeToken, nil
	case BillingModeRequest, BillingModeRules:
		return value, nil
	default:
		return "", gerror.New("unsupported model billing mode")
	}
}

// modelPriceValuesFromRule 把无条件规则的费率摊成公共价格字段。
// 带条件的规则（峰谷、上下文阶梯）不能当默认价用，返回 false。
func modelPriceValuesFromRule(rule syncedRule) (modelPriceValues, bool) {
	if ruleHasConditions(rule) {
		return modelPriceValues{}, false
	}
	rates := gjson.ParseBytes(rule.Rates)
	values := modelPriceValues{
		Input:       jsonPriceValue(rates.Get("inputPerMillion")),
		CachedInput: jsonPriceValue(rates.Get("cachedInputPerMillion")),
		CacheWrite:  jsonPriceValue(rates.Get("cacheWritePerMillion")),
		Output:      jsonPriceValue(rates.Get("outputPerMillion")),
		ImageInput:  jsonPriceValue(rates.Get("imageInputPerMillion")),
		AudioInput:  jsonPriceValue(rates.Get("audioInputPerMillion")),
		AudioOutput: jsonPriceValue(rates.Get("audioOutputPerMillion")),
		Request:     jsonPriceValue(rates.Get("request")),
	}
	return values, values.hasAny()
}

func jsonPriceValue(value gjson.Result) *float64 {
	if !value.Exists() {
		return nil
	}
	result := value.Float()
	return &result
}

// setPublicBillingMode 切换公共价格的计费模式。分档模型（上下文阶梯等）只有
// 走 rules 计费才能命中对应档位，否则规则虽然入库却不会参与计费。
// 记录不存在时创建，避免规则已写入却缺少计费入口。
func (s *sChannel) setPublicBillingMode(ctx context.Context, modelName, mode string) error {
	var current PublicModelView
	if err := dao.ModelPrices.Ctx(ctx).
		Where(dao.ModelPrices.Columns().PublicName, modelName).
		Scan(&current); err != nil && !isMissingPublicPriceError(err) {
		return gerror.Wrap(err, "load current public model price mode")
	}
	if current.PublicName == "" {
		if _, err := dao.ModelPrices.Ctx(ctx).Data(do.ModelPrices{PublicName: modelName, BillingMode: mode}).Insert(); err != nil {
			return gerror.Wrap(err, "create public model price for rules billing")
		}
		return nil
	}
	if current.BillingMode == mode {
		return nil
	}
	if _, err := dao.ModelPrices.Ctx(ctx).Where(dao.ModelPrices.Columns().PublicName, modelName).Data(do.ModelPrices{BillingMode: mode}).Update(); err != nil {
		return gerror.Wrap(err, "switch public model price to rules billing")
	}
	return nil
}
