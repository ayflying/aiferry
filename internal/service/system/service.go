package system

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gtime"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"github.com/yunloli/aiferry/internal/service/app"
)

const (
	resilienceSettingsKey = "channel_resilience"
	resilienceCacheKey    = "aiferry:system:channel-resilience"
	resilienceCacheTTL    = 5 * time.Minute
)

type Service struct {
	app *app.Service
}

type statusCodeRange struct {
	start int
	end   int
}

func New(appSvc *app.Service) *Service {
	return &Service{app: appSvc}
}

func DefaultResilienceSettings() adminapi.SystemResilienceSettingsInput {
	return adminapi.SystemResilienceSettingsInput{
		MaxFailoverAttempts:        3,
		RetryStatusCodes:           "401,403,404,408,429,500-599",
		HealthCheckEnabled:         false,
		HealthCheckMode:            "passive",
		HealthCheckIntervalMinutes: 5,
		RecoveryEnabled:            true,
		AutoDisableEnabled:         true,
		DisableLatencySeconds:      120,
		DisableStatusCodes:         "401,429",
		FailureKeywords: []string{
			"Your credit balance is too low",
			"This organization has been disabled.",
			"You exceeded your current quota",
			"Permission denied",
			"The security token included in the request is invalid",
			"Operation not allowed",
			"Your account is not authorized",
			"daily usage limit exceeded",
			"Insufficient account balance",
		},
	}
}

func (s *Service) Get(ctx context.Context) (adminapi.SystemResilienceSettingsInput, error) {
	if cached, err := s.app.Redis.Get(ctx, resilienceCacheKey).Bytes(); err == nil {
		if settings, decodeErr := decodeSettings(cached); decodeErr == nil {
			return settings, nil
		}
	}

	var row entity.SystemSettings
	if err := dao.SystemSettings.Ctx(ctx).Where(do.SystemSettings{SettingKey: resilienceSettingsKey}).Scan(&row); err != nil && !isNoRowsError(err) {
		return adminapi.SystemResilienceSettingsInput{}, gerror.Wrap(err, "load channel resilience settings")
	}
	if row.SettingKey == "" {
		return s.Update(ctx, DefaultResilienceSettings())
	}
	settings, err := decodeSettings([]byte(row.ValueJson))
	if err != nil {
		return adminapi.SystemResilienceSettingsInput{}, gerror.Wrap(err, "decode channel resilience settings")
	}
	_ = s.cache(ctx, settings)
	return settings, nil
}

func (s *Service) Update(ctx context.Context, input adminapi.SystemResilienceSettingsInput) (adminapi.SystemResilienceSettingsInput, error) {
	settings, err := normalizeSettings(input)
	if err != nil {
		return adminapi.SystemResilienceSettingsInput{}, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return adminapi.SystemResilienceSettingsInput{}, gerror.Wrap(err, "encode channel resilience settings")
	}
	result, err := dao.SystemSettings.Ctx(ctx).
		Where(do.SystemSettings{SettingKey: resilienceSettingsKey}).
		Data(do.SystemSettings{ValueJson: string(encoded)}).
		Update()
	if err != nil {
		return adminapi.SystemResilienceSettingsInput{}, gerror.Wrap(err, "update channel resilience settings")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, err = dao.SystemSettings.Ctx(ctx).Data(do.SystemSettings{SettingKey: resilienceSettingsKey, ValueJson: string(encoded)}).Insert(); err != nil {
			return adminapi.SystemResilienceSettingsInput{}, gerror.Wrap(err, "create channel resilience settings")
		}
	}
	_ = s.app.Redis.Del(ctx, resilienceCacheKey).Err()
	_ = s.cache(ctx, settings)
	return settings, nil
}

func (s *Service) DisableIfNeeded(ctx context.Context, input AutoDisableInput) (bool, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return false, err
	}
	return s.DisableIfNeededWithSettings(ctx, settings, input)
}

func (s *Service) DisableIfNeededWithSettings(ctx context.Context, settings adminapi.SystemResilienceSettingsInput, input AutoDisableInput) (bool, error) {
	if !settings.AutoDisableEnabled || !matchesAutoDisable(settings, input) {
		return false, nil
	}
	var channel entity.Channels
	if err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: input.ChannelID}).Scan(&channel); err != nil {
		return false, gerror.Wrap(err, "load channel for automatic disable")
	}
	if channel.Id == 0 {
		return false, gerror.New("channel not found")
	}
	if channel.AutoDisableEnabled != 1 {
		return false, nil
	}
	if channel.Status == 0 {
		return false, nil
	}
	if input.ChannelCredentialID > 0 {
		return s.disableCredential(ctx, input)
	}

	data := do.Channels{
		Status:                 0,
		AutoDisabledAt:         gtime.Now(),
		AutoDisabledReason:     autoDisableReason(input),
		AutoDisabledStatusCode: nil,
		AutoDisabledSource:     autoDisableSource(input.Source),
	}
	if input.Status > 0 {
		data.AutoDisabledStatusCode = input.Status
	} else {
		data.AutoDisabledStatusCode = gdb.Raw("NULL")
	}
	result, err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: input.ChannelID, Status: 1}).Data(data).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically disable channel")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.clearTransient(ctx, input.ChannelID)
	return true, nil
}

func (s *Service) disableCredential(ctx context.Context, input AutoDisableInput) (bool, error) {
	var credential entity.ChannelCredentials
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: input.ChannelCredentialID, ChannelId: input.ChannelID}).Scan(&credential); err != nil {
		return false, gerror.Wrap(err, "load channel credential for automatic disable")
	}
	if credential.Id == 0 || credential.Status == 0 {
		return false, nil
	}
	data := do.ChannelCredentials{
		Status:                 0,
		AutoDisabledAt:         gtime.Now(),
		AutoDisabledReason:     autoDisableReason(input),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     autoDisableSource(input.Source),
	}
	if input.Status > 0 {
		data.AutoDisabledStatusCode = input.Status
	}
	result, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credential.Id, Status: 1}).Data(data).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically disable channel credential")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.clearCredentialTransient(ctx, credential.Id)
	return true, nil
}

func (s *Service) RecoverIfAllowed(ctx context.Context, channelID uint64) (bool, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return false, err
	}
	if !settings.RecoveryEnabled {
		return false, nil
	}
	var channel entity.Channels
	if err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID}).Scan(&channel); err != nil {
		return false, gerror.Wrap(err, "load channel for automatic recovery")
	}
	if channel.Id == 0 {
		return false, gerror.New("channel not found")
	}
	if channel.Status != 0 || channel.AutoDisabledAt.IsZero() {
		return false, nil
	}
	result, err := dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID, Status: 0}).Data(do.Channels{
		Status:                 1,
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     gdb.Raw("NULL"),
	}).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically recover channel")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.clearTransient(ctx, channelID)
	return true, nil
}

func (s *Service) RecoverCredentialIfAllowed(ctx context.Context, credentialID uint64) (bool, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return false, err
	}
	if !settings.RecoveryEnabled {
		return false, nil
	}
	var credential entity.ChannelCredentials
	if err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credentialID}).Scan(&credential); err != nil {
		return false, gerror.Wrap(err, "load channel credential for automatic recovery")
	}
	if credential.Id == 0 || credential.Status != 0 || credential.AutoDisabledAt.IsZero() {
		return false, nil
	}
	result, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credential.Id, Status: 0}).Data(do.ChannelCredentials{
		Status:                 1,
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     gdb.Raw("NULL"),
	}).Update()
	if err != nil {
		return false, gerror.Wrap(err, "automatically recover channel credential")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return false, nil
	}
	s.clearCredentialTransient(ctx, credential.Id)
	return true, nil
}

func (s *Service) clearTransient(ctx context.Context, channelID uint64) {
	_ = s.app.Redis.Del(ctx, failureKey(channelID), cooldownKey(channelID)).Err()
	_ = s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
}

func (s *Service) clearCredentialTransient(ctx context.Context, credentialID uint64) {
	_ = s.app.Redis.Del(ctx,
		fmt.Sprintf("aiferry:credential:%d:failures", credentialID),
		fmt.Sprintf("aiferry:credential:%d:cooldown", credentialID),
	).Err()
	_ = s.app.Redis.Incr(ctx, "aiferry:routes:version").Err()
}

func (s *Service) cache(ctx context.Context, settings adminapi.SystemResilienceSettingsInput) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.app.Redis.Set(ctx, resilienceCacheKey, encoded, resilienceCacheTTL).Err()
}

func decodeSettings(value []byte) (adminapi.SystemResilienceSettingsInput, error) {
	settings := DefaultResilienceSettings()
	if err := json.Unmarshal(value, &settings); err != nil {
		return adminapi.SystemResilienceSettingsInput{}, err
	}
	return normalizeSettings(settings)
}

func normalizeSettings(input adminapi.SystemResilienceSettingsInput) (adminapi.SystemResilienceSettingsInput, error) {
	if input.MaxFailoverAttempts < 1 || input.MaxFailoverAttempts > 10 {
		return input, gerror.New("maxFailoverAttempts must be between 1 and 10")
	}
	var err error
	if input.RetryStatusCodes, _, err = normalizeStatusCodeRules(input.RetryStatusCodes); err != nil {
		return input, gerror.Wrap(err, "retryStatusCodes is invalid")
	}
	if input.HealthCheckMode == "" {
		input.HealthCheckMode = "passive"
	}
	if input.HealthCheckMode != "passive" && input.HealthCheckMode != "all" {
		return input, gerror.New("healthCheckMode must be passive or all")
	}
	if input.HealthCheckIntervalMinutes < 1 || input.HealthCheckIntervalMinutes > 1440 {
		return input, gerror.New("healthCheckIntervalMinutes must be between 1 and 1440")
	}
	if input.DisableLatencySeconds < 1 || input.DisableLatencySeconds > 3600 {
		return input, gerror.New("disableLatencySeconds must be between 1 and 3600")
	}
	if input.DisableStatusCodes, _, err = normalizeStatusCodeRules(input.DisableStatusCodes); err != nil {
		return input, gerror.Wrap(err, "disableStatusCodes is invalid")
	}
	input.FailureKeywords = normalizeKeywords(input.FailureKeywords)
	return input, nil
}

func MatchesStatusCodeRules(rules string, status int) bool {
	_, parsed, err := normalizeStatusCodeRules(rules)
	if err != nil {
		return false
	}
	for _, rule := range parsed {
		if status >= rule.start && status <= rule.end {
			return true
		}
	}
	return false
}

func normalizeStatusCodeRules(value string) (string, []statusCodeRange, error) {
	items := strings.Split(strings.TrimSpace(value), ",")
	rules := make([]statusCodeRange, 0, len(items))
	seen := make(map[statusCodeRange]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, "-")
		if len(parts) > 2 {
			return "", nil, gerror.New("each rule must be a status code or range")
		}
		start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || start < 100 || start > 599 {
			return "", nil, gerror.New("status codes must be between 100 and 599")
		}
		end := start
		if len(parts) == 2 {
			end, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || end < start || end > 599 {
				return "", nil, gerror.New("status code range is invalid")
			}
		}
		rule := statusCodeRange{start: start, end: end}
		if _, exists := seen[rule]; !exists {
			seen[rule] = struct{}{}
			rules = append(rules, rule)
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].start == rules[j].start {
			return rules[i].end < rules[j].end
		}
		return rules[i].start < rules[j].start
	})
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.start == rule.end {
			parts = append(parts, strconv.Itoa(rule.start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", rule.start, rule.end))
		}
	}
	return strings.Join(parts, ","), rules, nil
}

func normalizeKeywords(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		keyword := strings.TrimSpace(value)
		if keyword == "" || len(keyword) > 256 {
			continue
		}
		key := strings.ToLower(keyword)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, keyword)
	}
	return result
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func failureKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:failures", channelID)
}

func cooldownKey(channelID uint64) string {
	return fmt.Sprintf("aiferry:channel:%d:cooldown", channelID)
}
