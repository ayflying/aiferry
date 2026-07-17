package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/service/app"
	"github.com/yunloli/aiferry/internal/service/system"
	"github.com/yunloli/aiferry/internal/service/user"
)

const (
	lowBalanceReminderTTL = 24 * time.Hour
	channelBalancePrefix  = "aiferry:mail:channel-low-balance:"
)

type Service struct {
	app      *app.Service
	settings *system.Service
	users    *user.Service
}

func New(appSvc *app.Service, systemSvc *system.Service, userSvc *user.Service) *Service {
	return &Service{app: appSvc, settings: systemSvc, users: userSvc}
}

func (s *Service) NotifyLowBalance(ctx context.Context, userID uint64) {
	go s.notifyLowBalance(context.WithoutCancel(ctx), userID)
}

func (s *Service) NotifyChannelLowBalance(ctx context.Context, channelID uint64, channelName string, remaining float64) {
	go s.notifyChannelLowBalance(context.WithoutCancel(ctx), channelID, channelName, remaining)
}

func (s *Service) ClearChannelLowBalanceAlerts(ctx context.Context, channelID uint64) error {
	keys := make([]string, 0, len(channelBalanceThresholds))
	for _, threshold := range channelBalanceThresholds {
		keys = append(keys, channelBalanceReminderKey(channelID, threshold))
	}
	return gerror.Wrap(s.app.Redis.Del(ctx, keys...).Err(), "clear channel low balance mail alerts")
}

func (s *Service) SendTest(ctx context.Context, recipient string) error {
	recipient = strings.TrimSpace(recipient)
	if !validRecipient(recipient) {
		return gerror.New("测试收件人邮箱格式无效")
	}
	settings, err := s.settings.MailDeliverySettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return gerror.New("请先启用邮件提醒")
	}
	return send(settings, recipient, "AiFerry 邮件配置测试", "这是一封 AiFerry 发送的邮件配置测试。")
}

func (s *Service) notifyLowBalance(ctx context.Context, userID uint64) {
	profile, err := s.users.Profile(ctx, userID)
	if err != nil || strings.TrimSpace(profile.Email) == "" {
		return
	}
	settings, err := s.settings.MailDeliverySettings(ctx)
	if err != nil || !settings.Enabled || profile.Balance >= settings.Threshold {
		return
	}
	key := reminderKey(profile.Id, settings.Threshold)
	created, err := s.app.Redis.SetNX(ctx, key, "1", lowBalanceReminderTTL).Result()
	if err != nil || !created {
		return
	}
	subject, body := renderTemplates(settings, profile)
	if err = send(settings, profile.Email, subject, body); err != nil {
		_ = s.app.Redis.Del(ctx, key).Err()
	}
}

var channelBalanceThresholds = []float64{10, 5, 1}

func (s *Service) notifyChannelLowBalance(ctx context.Context, channelID uint64, channelName string, remaining float64) {
	settings, err := s.settings.MailDeliverySettings(ctx)
	if err != nil || !settings.Enabled {
		return
	}
	recipients, err := s.users.AdminEmails(ctx)
	if err != nil || len(recipients) == 0 {
		return
	}
	for _, threshold := range channelBalanceThresholds {
		if remaining >= threshold {
			continue
		}
		key := channelBalanceReminderKey(channelID, threshold)
		created, err := s.app.Redis.SetNX(ctx, key, "1", 0).Result()
		if err != nil || !created {
			continue
		}
		if !sendChannelLowBalance(settings, recipients, channelID, channelName, remaining, threshold) {
			_ = s.app.Redis.Del(ctx, key).Err()
		}
	}
}

func sendChannelLowBalance(settings system.MailDeliverySettings, recipients []string, channelID uint64, channelName string, remaining, threshold float64) bool {
	subject := fmt.Sprintf("AiFerry 渠道余额低于 $%.0f：%s", threshold, channelName)
	body := fmt.Sprintf("渠道 %s（ID：%d）当前上游余额为 $%.6f，已低于 $%.0f。", channelName, channelID, remaining, threshold)
	sent := false
	for _, recipient := range recipients {
		if err := send(settings, recipient, subject, body); err == nil {
			sent = true
		}
	}
	return sent
}

func send(settings system.MailDeliverySettings, recipient, subject, body string) error {
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	message := []byte("From: " + settings.From + "\r\n" +
		"To: " + recipient + "\r\n" +
		"Subject: " + encodeHeader(subject) + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body)
	if settings.Security == "tls" {
		connection, err := tls.Dial("tcp", address, &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return gerror.Wrap(err, "connect SMTP over TLS")
		}
		defer connection.Close()
		client, err := smtp.NewClient(connection, settings.Host)
		if err != nil {
			return gerror.Wrap(err, "create SMTP client")
		}
		defer client.Quit()
		return deliver(client, settings, recipient, message)
	}
	client, err := smtp.Dial(address)
	if err != nil {
		return gerror.Wrap(err, "connect SMTP")
	}
	defer client.Quit()
	if settings.Security == "starttls" {
		if err = client.StartTLS(&tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return gerror.Wrap(err, "start SMTP TLS")
		}
	}
	return deliver(client, settings, recipient, message)
}

func deliver(client *smtp.Client, settings system.MailDeliverySettings, recipient string, message []byte) error {
	if settings.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.Username, settings.Password, settings.Host)); err != nil {
			return gerror.Wrap(err, "authenticate SMTP")
		}
	}
	if err := client.Mail(settings.From); err != nil {
		return gerror.Wrap(err, "set SMTP sender")
	}
	if err := client.Rcpt(recipient); err != nil {
		return gerror.Wrap(err, "set SMTP recipient")
	}
	writer, err := client.Data()
	if err != nil {
		return gerror.Wrap(err, "open SMTP message")
	}
	if _, err = writer.Write(message); err != nil {
		_ = writer.Close()
		return gerror.Wrap(err, "write SMTP message")
	}
	return gerror.Wrap(writer.Close(), "send SMTP message")
}

func renderTemplates(settings system.MailDeliverySettings, profile user.Profile) (string, string) {
	replacer := strings.NewReplacer(
		"{nickname}", profile.Nickname,
		"{balance}", fmt.Sprintf("%.6f", profile.Balance),
		"{threshold}", fmt.Sprintf("%.6f", settings.Threshold),
	)
	return replacer.Replace(settings.SubjectTemplate), replacer.Replace(settings.BodyTemplate)
}

func reminderKey(userID uint64, threshold float64) string {
	return fmt.Sprintf("aiferry:mail:low-balance:%d:%.8f", userID, threshold)
}

func channelBalanceReminderKey(channelID uint64, threshold float64) string {
	return fmt.Sprintf("%s%d:%.0f", channelBalancePrefix, channelID, threshold)
}

func validRecipient(value string) bool {
	address, err := stdmail.ParseAddress(value)
	return err == nil && address.Address == value
}

func encodeHeader(value string) string {
	return mime.QEncoding.Encode("UTF-8", value)
}
