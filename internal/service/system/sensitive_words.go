package system

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	sensitiveWordSettingsKey = "sensitive_word_filter"
	sensitiveWordCacheKey    = "aiferry:system:sensitive-word-filter"
	sensitiveWordCacheTTL    = 5 * time.Minute
)

var ErrSensitiveWordBlocked = gerror.New("请求包含敏感关键词，已被系统拦截")

type promptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type promptContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func DefaultSensitiveWordSettings() adminapi.SensitiveWordSettingsInput {
	return adminapi.SensitiveWordSettingsInput{
		Enabled:         false,
		CheckUserPrompt: false,
		Keywords:        []string{},
	}
}

func (s *Service) GetSensitiveWordSettings(ctx context.Context) (adminapi.SensitiveWordSettingsInput, error) {
	if cached, err := s.app.Redis.Get(ctx, sensitiveWordCacheKey).Bytes(); err == nil {
		if settings, decodeErr := decodeSensitiveWordSettings(cached); decodeErr == nil {
			return settings, nil
		}
	}

	var row entity.SystemSettings
	if err := dao.SystemSettings.Ctx(ctx).Where(do.SystemSettings{SettingKey: sensitiveWordSettingsKey}).Scan(&row); err != nil && !isNoRowsError(err) {
		return adminapi.SensitiveWordSettingsInput{}, gerror.Wrap(err, "load sensitive word settings")
	}
	if row.SettingKey == "" {
		settings := DefaultSensitiveWordSettings()
		_ = s.cacheSensitiveWordSettings(ctx, settings)
		return settings, nil
	}
	settings, err := decodeSensitiveWordSettings([]byte(row.ValueJson))
	if err != nil {
		return adminapi.SensitiveWordSettingsInput{}, gerror.Wrap(err, "decode sensitive word settings")
	}
	_ = s.cacheSensitiveWordSettings(ctx, settings)
	return settings, nil
}

func (s *Service) UpdateSensitiveWordSettings(ctx context.Context, input adminapi.SensitiveWordSettingsInput) (adminapi.SensitiveWordSettingsInput, error) {
	settings := normalizeSensitiveWordSettings(input)
	encoded, err := json.Marshal(settings)
	if err != nil {
		return adminapi.SensitiveWordSettingsInput{}, gerror.Wrap(err, "encode sensitive word settings")
	}
	result, err := dao.SystemSettings.Ctx(ctx).
		Where(do.SystemSettings{SettingKey: sensitiveWordSettingsKey}).
		Data(do.SystemSettings{ValueJson: string(encoded)}).
		Update()
	if err != nil {
		return adminapi.SensitiveWordSettingsInput{}, gerror.Wrap(err, "update sensitive word settings")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, err = dao.SystemSettings.Ctx(ctx).Data(do.SystemSettings{SettingKey: sensitiveWordSettingsKey, ValueJson: string(encoded)}).Insert(); err != nil {
			return adminapi.SensitiveWordSettingsInput{}, gerror.Wrap(err, "create sensitive word settings")
		}
	}
	_ = s.app.Redis.Del(ctx, sensitiveWordCacheKey).Err()
	_ = s.cacheSensitiveWordSettings(ctx, settings)
	return settings, nil
}

func (s *Service) CheckSensitivePrompt(ctx context.Context, endpoint string, body []byte) error {
	settings, err := s.GetSensitiveWordSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled || !settings.CheckUserPrompt || len(settings.Keywords) == 0 {
		return nil
	}
	if matchesSensitivePrompt(endpoint, body, settings.Keywords) {
		return ErrSensitiveWordBlocked
	}
	return nil
}

func IsSensitiveWordBlocked(err error) bool {
	return errors.Is(err, ErrSensitiveWordBlocked)
}

func (s *Service) cacheSensitiveWordSettings(ctx context.Context, settings adminapi.SensitiveWordSettingsInput) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.app.Redis.Set(ctx, sensitiveWordCacheKey, encoded, sensitiveWordCacheTTL).Err()
}

func decodeSensitiveWordSettings(value []byte) (adminapi.SensitiveWordSettingsInput, error) {
	settings := DefaultSensitiveWordSettings()
	if err := json.Unmarshal(value, &settings); err != nil {
		return adminapi.SensitiveWordSettingsInput{}, err
	}
	return normalizeSensitiveWordSettings(settings), nil
}

func normalizeSensitiveWordSettings(input adminapi.SensitiveWordSettingsInput) adminapi.SensitiveWordSettingsInput {
	input.Keywords = normalizeKeywords(input.Keywords)
	if input.Keywords == nil {
		input.Keywords = []string{}
	}
	return input
}

func matchesSensitivePrompt(endpoint string, body []byte, keywords []string) bool {
	texts := sensitivePromptTexts(endpoint, body)
	if len(texts) == 0 {
		return false
	}
	lowerKeywords := make([]string, 0, len(keywords))
	for _, keyword := range normalizeKeywords(keywords) {
		lowerKeywords = append(lowerKeywords, strings.ToLower(keyword))
	}
	for _, text := range texts {
		lowerText := strings.ToLower(text)
		for _, keyword := range lowerKeywords {
			if strings.Contains(lowerText, keyword) {
				return true
			}
		}
	}
	return false
}

func sensitivePromptTexts(endpoint string, body []byte) []string {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	switch endpoint {
	case "/chat/completions":
		return userMessageTexts(payload["messages"])
	case "/responses":
		return responseInputTexts(payload["input"])
	case "/embeddings":
		return embeddingInputTexts(payload["input"])
	default:
		return nil
	}
}

func userMessageTexts(raw json.RawMessage) []string {
	var messages []promptMessage
	if len(raw) == 0 || json.Unmarshal(raw, &messages) != nil {
		return nil
	}
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "user") {
			texts = append(texts, contentTexts(message.Content)...)
		}
	}
	return texts
}

func responseInputTexts(raw json.RawMessage) []string {
	var text string
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &text); err == nil {
		return []string{text}
	}
	return userMessageTexts(raw)
}

func embeddingInputTexts(raw json.RawMessage) []string {
	var text string
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &text); err == nil {
		return []string{text}
	}
	var texts []string
	if err := json.Unmarshal(raw, &texts); err != nil {
		return nil
	}
	return texts
}

func contentTexts(raw json.RawMessage) []string {
	var text string
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &text); err == nil {
		return []string{text}
	}
	var items []promptContentItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(item.Type, "text") || strings.EqualFold(item.Type, "input_text") {
			texts = append(texts, item.Text)
		}
	}
	return texts
}
