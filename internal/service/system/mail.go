package system

import (
	"context"
	"encoding/json"
	"math"
	stdmail "net/mail"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const mailSettingsKey = "mail_delivery"

type MailSettings struct {
	Enabled            bool    `json:"enabled"`
	Host               string  `json:"host"`
	Port               int     `json:"port"`
	Username           string  `json:"username"`
	PasswordConfigured bool    `json:"passwordConfigured"`
	From               string  `json:"from"`
	Security           string  `json:"security"`
	Threshold          float64 `json:"threshold"`
	SubjectTemplate    string  `json:"subjectTemplate"`
	BodyTemplate       string  `json:"bodyTemplate"`
}

type MailDeliverySettings struct {
	MailSettings
	Password string
}

type storedMailSettings struct {
	Enabled         bool    `json:"enabled"`
	Host            string  `json:"host"`
	Port            int     `json:"port"`
	Username        string  `json:"username"`
	PasswordCipher  string  `json:"passwordCipher"`
	From            string  `json:"from"`
	Security        string  `json:"security"`
	Threshold       float64 `json:"threshold"`
	SubjectTemplate string  `json:"subjectTemplate"`
	BodyTemplate    string  `json:"bodyTemplate"`
}

func DefaultMailSettings() MailSettings {
	return MailSettings{
		Enabled: false, Port: 587, Security: "starttls", Threshold: 5,
		SubjectTemplate: "AiFerry 余额不足提醒",
		BodyTemplate:    "您好，{nickname}：\n\n您的 AiFerry 余额为 ${balance}，已低于提醒阈值 ${threshold}。\n\n请及时充值以避免调用中断。",
	}
}

func (s *Service) GetMailSettings(ctx context.Context) (MailSettings, error) {
	stored, err := s.loadStoredMailSettings(ctx)
	if err != nil {
		return MailSettings{}, err
	}
	return mailSettingsView(stored), nil
}

func (s *Service) UpdateMailSettings(ctx context.Context, input adminapi.MailSettingsInput) (MailSettings, error) {
	current, err := s.loadStoredMailSettings(ctx)
	if err != nil {
		return MailSettings{}, err
	}
	stored, err := normalizeMailSettings(input, current)
	if err != nil {
		return MailSettings{}, err
	}
	if input.Password != nil {
		password := strings.TrimSpace(*input.Password)
		if password == "" {
			stored.PasswordCipher = ""
		} else {
			stored.PasswordCipher, err = s.app.Secrets.Encrypt(password)
			if err != nil {
				return MailSettings{}, gerror.Wrap(err, "encrypt SMTP password")
			}
		}
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return MailSettings{}, gerror.Wrap(err, "encode mail settings")
	}
	if err = s.saveMailSettings(ctx, string(encoded)); err != nil {
		return MailSettings{}, err
	}
	return mailSettingsView(stored), nil
}

func (s *Service) MailDeliverySettings(ctx context.Context) (MailDeliverySettings, error) {
	stored, err := s.loadStoredMailSettings(ctx)
	if err != nil {
		return MailDeliverySettings{}, err
	}
	result := MailDeliverySettings{MailSettings: mailSettingsView(stored)}
	if stored.PasswordCipher == "" {
		return result, nil
	}
	result.Password, err = s.app.Secrets.Decrypt(stored.PasswordCipher)
	if err != nil {
		return MailDeliverySettings{}, gerror.Wrap(err, "decrypt SMTP password")
	}
	return result, nil
}

func (s *Service) loadStoredMailSettings(ctx context.Context) (storedMailSettings, error) {
	var row entity.SystemSettings
	if err := dao.SystemSettings.Ctx(ctx).Where(do.SystemSettings{SettingKey: mailSettingsKey}).Scan(&row); err != nil {
		return storedMailSettings{}, gerror.Wrap(err, "load mail settings")
	}
	if row.SettingKey == "" {
		return storedFromView(DefaultMailSettings()), nil
	}
	var stored storedMailSettings
	if err := json.Unmarshal([]byte(row.ValueJson), &stored); err != nil {
		return storedMailSettings{}, gerror.Wrap(err, "decode mail settings")
	}
	return stored, nil
}

func (s *Service) saveMailSettings(ctx context.Context, value string) error {
	result, err := dao.SystemSettings.Ctx(ctx).
		Where(do.SystemSettings{SettingKey: mailSettingsKey}).
		Data(do.SystemSettings{ValueJson: value}).
		Update()
	if err != nil {
		return gerror.Wrap(err, "update mail settings")
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, err = dao.SystemSettings.Ctx(ctx).Data(do.SystemSettings{SettingKey: mailSettingsKey, ValueJson: value}).Insert(); err != nil {
			return gerror.Wrap(err, "create mail settings")
		}
	}
	return nil
}

func normalizeMailSettings(input adminapi.MailSettingsInput, current storedMailSettings) (storedMailSettings, error) {
	settings := storedMailSettings{
		Enabled: input.Enabled, Host: strings.TrimSpace(input.Host), Port: input.Port,
		Username: strings.TrimSpace(input.Username), PasswordCipher: current.PasswordCipher,
		From: strings.TrimSpace(input.From), Security: strings.TrimSpace(input.Security),
		Threshold: input.Threshold, SubjectTemplate: strings.TrimSpace(input.SubjectTemplate), BodyTemplate: strings.TrimSpace(input.BodyTemplate),
	}
	if settings.Security == "" {
		settings.Security = "starttls"
	}
	if settings.Security != "none" && settings.Security != "starttls" && settings.Security != "tls" {
		return settings, gerror.New("SMTP security must be none, starttls, or tls")
	}
	if math.IsNaN(settings.Threshold) || math.IsInf(settings.Threshold, 0) || settings.Threshold < 0 || settings.Threshold > 1_000_000 {
		return settings, gerror.New("余额邮件提醒阈值必须介于 0 和 1000000 之间")
	}
	if !settings.Enabled {
		return settings, nil
	}
	if settings.Host == "" || len(settings.Host) > 255 {
		return settings, gerror.New("SMTP 主机不能为空且不能超过 255 个字符")
	}
	if settings.Port < 1 || settings.Port > 65535 {
		return settings, gerror.New("SMTP 端口必须介于 1 和 65535 之间")
	}
	if settings.From == "" || !validMailAddress(settings.From) {
		return settings, gerror.New("发件人邮箱格式无效")
	}
	if settings.Username != "" && settings.PasswordCipher == "" {
		return settings, gerror.New("SMTP 用户名已填写，请同时配置密码")
	}
	if settings.SubjectTemplate == "" || len(settings.SubjectTemplate) > 255 {
		return settings, gerror.New("邮件主题模板长度应为 1 到 255 个字符")
	}
	if settings.BodyTemplate == "" || len(settings.BodyTemplate) > 20_000 {
		return settings, gerror.New("邮件正文模板长度应为 1 到 20000 个字符")
	}
	return settings, nil
}

func mailSettingsView(stored storedMailSettings) MailSettings {
	return MailSettings{
		Enabled: stored.Enabled, Host: stored.Host, Port: stored.Port, Username: stored.Username,
		PasswordConfigured: stored.PasswordCipher != "", From: stored.From, Security: stored.Security,
		Threshold: stored.Threshold, SubjectTemplate: stored.SubjectTemplate, BodyTemplate: stored.BodyTemplate,
	}
}

func storedFromView(settings MailSettings) storedMailSettings {
	return storedMailSettings{
		Enabled: settings.Enabled, Host: settings.Host, Port: settings.Port, Username: settings.Username,
		From: settings.From, Security: settings.Security, Threshold: settings.Threshold,
		SubjectTemplate: settings.SubjectTemplate, BodyTemplate: settings.BodyTemplate,
	}
}

func validMailAddress(value string) bool {
	address, err := stdmail.ParseAddress(value)
	return err == nil && address.Address == value
}
