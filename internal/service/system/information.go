package system

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const (
	systemInformationSettingsKey = "system_information"
	systemInformationCacheKey    = "aiferry:system:information"
	systemInformationCacheTTL    = 5 * time.Minute

	systemNameLimit        = 96
	systemURLLimit         = 2048
	systemFooterLimit      = 4096
	systemPageContentLimit = 65535
)

func DefaultSystemInformation() adminapi.SystemInformationInput {
	return adminapi.SystemInformationInput{SystemName: "AiFerry"}
}

func (s *Service) GetSystemInformation(ctx context.Context) (adminapi.SystemInformationInput, error) {
	if cached, err := s.app.Redis.Get(ctx, systemInformationCacheKey).Bytes(); err == nil {
		if settings, decodeErr := decodeSystemInformation(cached); decodeErr == nil {
			return settings, nil
		}
	}

	var row entity.SystemSettings
	if err := dao.SystemSettings.Ctx(ctx).Where(do.SystemSettings{SettingKey: systemInformationSettingsKey}).Scan(&row); err != nil && !isNoRowsError(err) {
		return adminapi.SystemInformationInput{}, gerror.Wrap(err, "load system information")
	}
	if row.SettingKey == "" {
		settings := DefaultSystemInformation()
		_ = s.cacheSystemInformation(ctx, settings)
		return settings, nil
	}
	settings, err := decodeSystemInformation([]byte(row.ValueJson))
	if err != nil {
		return adminapi.SystemInformationInput{}, gerror.Wrap(err, "decode system information")
	}
	_ = s.cacheSystemInformation(ctx, settings)
	return settings, nil
}

func (s *Service) UpdateSystemInformation(ctx context.Context, input adminapi.SystemInformationInput) (adminapi.SystemInformationInput, error) {
	settings, err := normalizeSystemInformation(input)
	if err != nil {
		return adminapi.SystemInformationInput{}, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return adminapi.SystemInformationInput{}, gerror.Wrap(err, "encode system information")
	}
	result, err := dao.SystemSettings.Ctx(ctx).
		Where(do.SystemSettings{SettingKey: systemInformationSettingsKey}).
		Data(do.SystemSettings{ValueJson: string(encoded)}).
		Update()
	if err != nil {
		return adminapi.SystemInformationInput{}, gerror.Wrap(err, "update system information")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, err = dao.SystemSettings.Ctx(ctx).Data(do.SystemSettings{SettingKey: systemInformationSettingsKey, ValueJson: string(encoded)}).Insert(); err != nil {
			return adminapi.SystemInformationInput{}, gerror.Wrap(err, "create system information")
		}
	}
	_ = s.app.Redis.Del(ctx, systemInformationCacheKey).Err()
	_ = s.cacheSystemInformation(ctx, settings)
	return settings, nil
}

// ResolveSystemInformation fills the public server URL without changing the stored configuration.
func (s *Service) ResolveSystemInformation(ctx context.Context, fallbackServerURL string) (adminapi.SystemInformationInput, error) {
	settings, err := s.GetSystemInformation(ctx)
	if err != nil {
		return adminapi.SystemInformationInput{}, err
	}
	if settings.ServerURL != "" {
		return settings, nil
	}
	settings.ServerURL, err = normalizeRootHTTPURL(fallbackServerURL, false)
	if err != nil {
		return adminapi.SystemInformationInput{}, gerror.Wrap(err, "resolve system server URL")
	}
	return settings, nil
}

func (s *Service) cacheSystemInformation(ctx context.Context, settings adminapi.SystemInformationInput) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.app.Redis.Set(ctx, systemInformationCacheKey, encoded, systemInformationCacheTTL).Err()
}

func decodeSystemInformation(value []byte) (adminapi.SystemInformationInput, error) {
	settings := DefaultSystemInformation()
	if err := json.Unmarshal(value, &settings); err != nil {
		return adminapi.SystemInformationInput{}, err
	}
	return normalizeSystemInformation(settings)
}

func normalizeSystemInformation(input adminapi.SystemInformationInput) (adminapi.SystemInformationInput, error) {
	input.SystemName = strings.TrimSpace(input.SystemName)
	input.ServerURL = strings.TrimSpace(input.ServerURL)
	input.LogoURL = strings.TrimSpace(input.LogoURL)
	input.Footer = strings.TrimSpace(input.Footer)

	if input.SystemName == "" {
		return input, gerror.New("系统名称不能为空")
	}
	if utf8.RuneCountInString(input.SystemName) > systemNameLimit || strings.ContainsAny(input.SystemName, "\r\n") {
		return input, gerror.New("系统名称不能超过 96 个字符或包含换行")
	}
	var err error
	if input.ServerURL, err = normalizeRootHTTPURL(input.ServerURL, true); err != nil {
		return input, err
	}
	if err = validateAbsoluteHTTPURL(input.LogoURL, true); err != nil {
		return input, gerror.Wrap(err, "徽标 URL 无效")
	}
	if utf8.RuneCountInString(input.Footer) > systemFooterLimit {
		return input, gerror.New("页脚内容不能超过 4096 个字符")
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "关于", value: input.About},
		{label: "首页内容", value: input.HomeContent},
		{label: "用户协议", value: input.UserAgreement},
		{label: "隐私政策", value: input.PrivacyPolicy},
	} {
		label, value := field.label, field.value
		if utf8.RuneCountInString(value) > systemPageContentLimit {
			return input, gerror.Newf("%s不能超过 65535 个字符", label)
		}
	}
	return input, nil
}

func normalizeRootHTTPURL(value string, allowEmpty bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" && allowEmpty {
		return "", nil
	}
	if len(value) > systemURLLimit {
		return "", gerror.New("服务器地址不能超过 2048 个字符")
	}
	parsed, err := parseAbsoluteHTTPURL(value)
	if err != nil {
		return "", gerror.New("服务器地址必须是根级绝对 HTTP(S) URL")
	}
	if escapedPath := parsed.EscapedPath(); escapedPath != "" && escapedPath != "/" {
		return "", gerror.New("服务器地址必须是根级绝对 HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", gerror.New("服务器地址不能包含查询参数或片段")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return parsed.String(), nil
}

func validateAbsoluteHTTPURL(value string, allowEmpty bool) error {
	value = strings.TrimSpace(value)
	if value == "" && allowEmpty {
		return nil
	}
	if len(value) > systemURLLimit {
		return gerror.New("URL 不能超过 2048 个字符")
	}
	_, err := parseAbsoluteHTTPURL(value)
	if err != nil {
		return gerror.New("必须是绝对 HTTP(S) URL")
	}
	return nil
}

func parseAbsoluteHTTPURL(value string) (*url.URL, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, gerror.New("URL contains control characters")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return nil, gerror.New("invalid absolute URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, gerror.New("unsupported URL scheme")
	}
	return parsed, nil
}
