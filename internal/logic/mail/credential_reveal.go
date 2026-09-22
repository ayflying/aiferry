package mail

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/redis/go-redis/v9"
)

// 上游密钥明文查看的邮箱验证门：
// 首次点「显示」需邮箱收验证码验证一次；验证通过后 10 分钟窗口内
// 可反复查看任意渠道的上游密钥明文，不再重复验证。验证码 5 分钟有效，
// 同一用户 60 秒内只允许发一次。
const (
	credentialRevealCodeTTL      = 5 * time.Minute
	credentialRevealSendCooldown = 60 * time.Second
	credentialRevealVerifiedTTL  = 10 * time.Minute
)

// CredentialRevealStatus 是密钥明文查看的验证状态视图。
type CredentialRevealStatus struct {
	// Verified 为 true 表示 10 分钟验证窗口仍有效，可直接揭示密钥。
	Verified bool `json:"verified"`
	// EmailReady 为 false 表示用户档案未填邮箱，无法发送验证码。
	EmailReady bool `json:"emailReady"`
	// EmailMasked 是脱敏邮箱（如 a***@example.com），供验证框展示投递目标。
	EmailMasked string `json:"emailMasked"`
	// ExpiresInSeconds 是验证窗口剩余秒数（仅 Verified 时有意义）。
	ExpiresInSeconds int64 `json:"expiresInSeconds"`
}

func credentialRevealCodeKey(userID uint64) string {
	return fmt.Sprintf("aiferry:mail:reveal-code:%d", userID)
}

func credentialRevealCooldownKey(userID uint64) string {
	return fmt.Sprintf("aiferry:mail:reveal-code-cooldown:%d", userID)
}

func credentialRevealVerifiedKey(userID uint64) string {
	return fmt.Sprintf("aiferry:mail:reveal-verified:%d", userID)
}

// CredentialRevealStatus 返回当前用户的密钥揭示验证状态与脱敏邮箱。
func (s *sMail) CredentialRevealStatus(ctx context.Context, userID uint64) (CredentialRevealStatus, error) {
	if userID == 0 {
		return CredentialRevealStatus{}, gerror.New("用户不能为空")
	}
	profile, err := s.users.Profile(ctx, userID)
	if err != nil {
		return CredentialRevealStatus{}, err
	}
	status := CredentialRevealStatus{
		EmailReady:  strings.TrimSpace(profile.Email) != "",
		EmailMasked: maskEmail(profile.Email),
	}
	ttl, err := s.app.Redis.TTL(ctx, credentialRevealVerifiedKey(userID)).Result()
	if err == nil && ttl > 0 {
		status.Verified = true
		status.ExpiresInSeconds = int64(ttl.Seconds())
	}
	return status, nil
}

// CredentialRevealVerified 返回 10 分钟验证窗口是否仍有效（揭示接口的权威门）。
func (s *sMail) CredentialRevealVerified(ctx context.Context, userID uint64) (bool, error) {
	if userID == 0 {
		return false, gerror.New("用户不能为空")
	}
	ttl, err := s.app.Redis.TTL(ctx, credentialRevealVerifiedKey(userID)).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, gerror.Wrap(err, "read credential reveal verification")
	}
	return ttl > 0, nil
}

// SendCredentialRevealCode 向当前用户档案邮箱发送 6 位验证码。
// 同一用户 60 秒内只允许发送一次；验证码 5 分钟有效。
func (s *sMail) SendCredentialRevealCode(ctx context.Context, userID uint64) (CredentialRevealStatus, error) {
	if userID == 0 {
		return CredentialRevealStatus{}, gerror.New("用户不能为空")
	}
	profile, err := s.users.Profile(ctx, userID)
	if err != nil {
		return CredentialRevealStatus{}, err
	}
	email := strings.TrimSpace(profile.Email)
	if email == "" {
		return CredentialRevealStatus{}, gerror.New("尚未填写邮箱，请先在「个人设置」中填写邮箱再发送验证码")
	}
	settings, err := s.settings.MailDeliverySettings(ctx)
	if err != nil {
		return CredentialRevealStatus{}, err
	}
	if err = validateTestSettings(settings); err != nil {
		return CredentialRevealStatus{}, gerror.Wrap(err, "邮件服务未配置")
	}
	cooldownKey := credentialRevealCooldownKey(userID)
	created, err := s.app.Redis.SetNX(ctx, cooldownKey, "1", credentialRevealSendCooldown).Result()
	if err != nil {
		return CredentialRevealStatus{}, gerror.Wrap(err, "check credential reveal send cooldown")
	}
	if !created {
		remaining, ttlErr := s.app.Redis.TTL(ctx, cooldownKey).Result()
		seconds := int64(credentialRevealSendCooldown.Seconds())
		if ttlErr == nil && remaining > 0 {
			seconds = int64(remaining.Seconds()) + 1
		}
		return CredentialRevealStatus{}, gerror.Newf("发送太频繁，请 %d 秒后再试", seconds)
	}
	code, err := generateCredentialRevealCode()
	if err != nil {
		return CredentialRevealStatus{}, err
	}
	codeKey := credentialRevealCodeKey(userID)
	if err = s.app.Redis.Set(ctx, codeKey, code, credentialRevealCodeTTL).Err(); err != nil {
		_ = s.app.Redis.Del(ctx, cooldownKey).Err()
		return CredentialRevealStatus{}, gerror.Wrap(err, "store credential reveal code")
	}
	subject := fmt.Sprintf("%s 密钥查看验证码", s.systemName(ctx))
	body := fmt.Sprintf(
		"你正在查看上游密钥明文，验证码：%s\n\n验证码 %d 分钟内有效；验证通过后 %d 分钟内可反复查看，无需重复验证。若非本人操作请忽略本邮件。",
		code,
		int(credentialRevealCodeTTL.Minutes()),
		int(credentialRevealVerifiedTTL.Minutes()),
	)
	if err = send(settings, email, subject, body); err != nil {
		_ = s.app.Redis.Del(ctx, codeKey, cooldownKey).Err()
		return CredentialRevealStatus{}, gerror.Wrap(err, "send credential reveal code")
	}
	return s.CredentialRevealStatus(ctx, userID)
}

// VerifyCredentialRevealCode 校验验证码；通过后开启 10 分钟验证窗口并作废该码。
func (s *sMail) VerifyCredentialRevealCode(ctx context.Context, userID uint64, code string) error {
	if userID == 0 {
		return gerror.New("用户不能为空")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return gerror.New("请输入验证码")
	}
	codeKey := credentialRevealCodeKey(userID)
	stored, err := s.app.Redis.Get(ctx, codeKey).Result()
	if err != nil {
		if err == redis.Nil {
			return gerror.New("验证码已过期，请重新获取")
		}
		return gerror.Wrap(err, "read credential reveal code")
	}
	if stored != code {
		return gerror.New("验证码错误")
	}
	// 一码一用：验码通过即作废，再开 10 分钟窗口。
	_ = s.app.Redis.Del(ctx, codeKey).Err()
	if err = s.app.Redis.Set(ctx, credentialRevealVerifiedKey(userID), "1", credentialRevealVerifiedTTL).Err(); err != nil {
		return gerror.Wrap(err, "open credential reveal window")
	}
	return nil
}

// generateCredentialRevealCode 生成 6 位数字验证码（crypto/rand，均匀取模）。
func generateCredentialRevealCode() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", gerror.Wrap(err, "generate credential reveal code")
	}
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(buf[:])%1000000), nil
}

// maskEmail 把邮箱脱敏为 a***@example.com 形式，本地部分只保留首字符。
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return ""
	}
	local, domain := email[:at], email[at+1:]
	runes := []rune(local)
	if len(runes) <= 1 {
		return "***@" + domain
	}
	return string(runes[0]) + "***@" + domain
}
