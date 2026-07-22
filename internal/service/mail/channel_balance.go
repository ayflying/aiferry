package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/yunloli/aiferry/internal/service/system"
)

const channelBalancePrefix = "aiferry:mail:channel-low-balance:"

func (s *Service) notifyChannelLowBalance(ctx context.Context, channelID uint64, channelName string, remaining float64, currency string) {
	settings, err := s.settings.MailDeliverySettings(ctx)
	if err != nil || !settings.ChannelAlertEnabled {
		return
	}
	thresholds, err := system.ParseChannelBalanceThresholds(settings.ChannelBalanceThresholds)
	if err != nil {
		return
	}
	for _, threshold := range thresholds {
		if remaining >= threshold {
			_ = s.app.Redis.Del(ctx, channelBalanceReminderKey(channelID, threshold)).Err()
		}
	}
	threshold, alerted := lowestTriggeredChannelBalanceThreshold(remaining, thresholds)
	if !alerted {
		return
	}
	recipients, err := s.users.AdminEmails(ctx)
	if err != nil || len(recipients) == 0 {
		return
	}
	key := channelBalanceReminderKey(channelID, threshold)
	created, err := s.app.Redis.SetNX(ctx, key, "1", 0).Result()
	if err != nil || !created {
		return
	}
	if !sendChannelLowBalance(settings, recipients, s.systemName(ctx), channelID, channelName, remaining, threshold, currency) {
		_ = s.app.Redis.Del(ctx, key).Err()
	}
}

func lowestTriggeredChannelBalanceThreshold(remaining float64, thresholds []float64) (float64, bool) {
	var (
		selected float64
		found    bool
	)
	for _, threshold := range thresholds {
		if remaining >= threshold || (found && threshold >= selected) {
			continue
		}
		selected = threshold
		found = true
	}
	return selected, found
}

func (s *Service) ClearChannelLowBalanceReminders(ctx context.Context, channelID uint64) error {
	if channelID == 0 {
		return nil
	}
	settings, err := s.settings.MailDeliverySettings(ctx)
	if err != nil {
		return err
	}
	thresholds, err := system.ParseChannelBalanceThresholds(settings.ChannelBalanceThresholds)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(thresholds))
	for _, threshold := range thresholds {
		keys = append(keys, channelBalanceReminderKey(channelID, threshold))
	}
	if len(keys) == 0 {
		return nil
	}
	return gerror.Wrap(s.app.Redis.Del(ctx, keys...).Err(), "clear channel low-balance reminders")
}

func sendChannelLowBalance(settings system.MailDeliverySettings, recipients []string, systemName string, channelID uint64, channelName string, remaining, threshold float64, currency string) bool {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	thresholdText := system.ChannelBalanceThresholdText(threshold)
	subject := fmt.Sprintf("%s 渠道余额低于 %s %s：%s", systemName, thresholdText, currency, channelName)
	body := fmt.Sprintf("渠道 %s（ID：%d）当前上游余额为 %.6f %s，已低于 %s %s。", channelName, channelID, remaining, currency, thresholdText, currency)
	sent := false
	for _, recipient := range recipients {
		if err := send(settings, recipient, subject, body); err == nil {
			sent = true
		}
	}
	return sent
}

func channelBalanceReminderKey(channelID uint64, threshold float64) string {
	return fmt.Sprintf("%s%d:%s", channelBalancePrefix, channelID, system.ChannelBalanceThresholdText(threshold))
}
