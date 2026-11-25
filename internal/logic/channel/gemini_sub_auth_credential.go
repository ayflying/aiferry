package channel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

const geminiCredentialPrefix = "aiferry:gemini-oauth:v1:"
const geminiRefreshAhead = 60 * time.Second

type geminiSubscriptionCredential struct {
	Subject      string
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

func geminiAccountHash(subject string) string {
	return upstreamKeyHash("gemini-oauth-account:" + subject)
}

func geminiCredentialAAD(channelID uint64, hash string) string {
	return fmt.Sprintf("credential:%d:%s", channelID, hash)
}

func (s *sChannel) saveGeminiCredential(ctx context.Context, cfg geminiOAuthConfig, channelID uint64, credential geminiSubscriptionCredential) (uint64, error) {
	hash := geminiAccountHash(credential.Subject)
	sealed, err := cfg.seal(geminiCredentialAAD(channelID, hash), credential)
	if err != nil {
		return 0, err
	}
	var id uint64
	err = dao.Channels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		var saveErr error
		id, saveErr = persistGeminiCredential(txCtx, channelID, hash, geminiCredentialPrefix+sealed)
		return saveErr
	})
	if err != nil {
		return 0, err
	}
	s.invalidateCredentialCache(ctx)
	s.InvalidateListCache(ctx)
	_ = s.invalidateRoutes(ctx)
	return id, nil
}

func persistGeminiCredential(ctx context.Context, channelID uint64, hash, cipher string) (uint64, error) {
	// MySQL 先锁渠道行，串行化同一渠道的并发首次登录。SQLite 的事务读取旧
	// 快照后无法与另一写事务同时提交；发生竞态时由轮询重试，不重复换票。
	var channel entity.Channels
	model := dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID, Type: GeminiSubscriptionChannelType})
	if dao.Channels.DB().GetConfig().Type != "sqlite" {
		model = model.LockUpdate()
	}
	if err := model.Scan(&channel); err != nil || channel.Id == 0 {
		return 0, gerror.New("Gemini 凭据所属订阅渠道不存在")
	}
	var row credentialRow
	where := do.ChannelCredentials{ChannelId: channelID}
	if err := dao.ChannelCredentials.Ctx(ctx).Where(where).Scan(&row); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, gerror.New("读取 Gemini 登录凭据失败")
	}
	if row.Id != 0 && row.KeyHash != hash {
		return 0, gerror.New("Gemini 订阅渠道只允许一个账号；如需更换账号，请先删除现有凭据")
	}
	where.KeyHash = hash
	id := row.Id
	if id == 0 {
		data := do.ChannelCredentials{ChannelId: channelID, ApiKeyCipher: cipher, KeyHash: hash, KeyPrefix: "Gemini OAuth " + maskedCredentialPrefix(hash), Status: 1}
		created, err := dao.ChannelCredentials.Ctx(ctx).Data(data).InsertAndGetId()
		if err != nil || created <= 0 {
			return 0, gerror.New("保存 Gemini 登录凭据失败，请重试")
		}
		id = uint64(created)
	} else {
		// 同账号重新授权更新原凭据，保留备注、绑定和手动启停状态。
		where.Id = id
		updated, err := dao.ChannelCredentials.Ctx(ctx).Where(where).Data(do.ChannelCredentials{ApiKeyCipher: cipher}).Update()
		if err != nil {
			return 0, gerror.New("更新 Gemini 登录凭据失败")
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return 0, gerror.New("Gemini 凭据已变更，请重试")
		}
	}
	return id, nil
}

func (s *sChannel) readGeminiCredential(ctx context.Context, cfg geminiOAuthConfig, channelID, credentialID uint64) (credentialRow, geminiSubscriptionCredential, error) {
	var credential geminiSubscriptionCredential
	row, err := s.credentialByID(ctx, channelID, credentialID)
	if err != nil || row.Status != 1 || !strings.HasPrefix(row.ApiKeyCipher, geminiCredentialPrefix) {
		return row, credential, gerror.New("Gemini 登录凭据不存在、未启用或无效，请检查渠道凭据")
	}
	err = cfg.open(geminiCredentialAAD(channelID, row.KeyHash), strings.TrimPrefix(row.ApiKeyCipher, geminiCredentialPrefix), &credential)
	if err != nil {
		return row, credential, err
	}
	if credential.Subject == "" || row.KeyHash != geminiAccountHash(credential.Subject) || !validGeminiOAuthToken(credential.AccessToken) || !validGeminiOAuthToken(credential.RefreshToken) || credential.ExpiresAt <= 0 {
		return row, credential, gerror.New("Gemini 登录凭据无效，请重新授权")
	}
	return row, credential, nil
}

// GeminiSubscriptionAccessToken 仅供服务内部在已选定的订阅渠道上生成
// 官方请求使用。调用方必须同时使用 GeminiSubscriptionHTTPClient 并限制目标端点。
func (s *sChannel) GeminiSubscriptionAccessToken(ctx context.Context, channelID, credentialID uint64, credentialCipher string) (string, error) {
	if channelID == 0 || credentialID == 0 || !strings.HasPrefix(credentialCipher, geminiCredentialPrefix) {
		return "", gerror.New("Gemini 订阅渠道需要通过 Google OAuth 登录授权")
	}
	cfg, err := readGeminiOAuthConfig()
	if err != nil {
		return "", err
	}
	_, credential, err := s.readGeminiCredential(ctx, cfg, channelID, credentialID)
	if err != nil {
		return "", err
	}
	if geminiCredentialFresh(credential) {
		return credential.AccessToken, nil
	}
	return s.refreshGeminiCredential(ctx, cfg, channelID, credentialID)
}

func geminiCredentialFresh(credential geminiSubscriptionCredential) bool {
	return credential.ExpiresAt > time.Now().Add(geminiRefreshAhead).Unix()
}

func (s *sChannel) refreshGeminiCredential(ctx context.Context, cfg geminiOAuthConfig, channelID, credentialID uint64) (string, error) {
	if s.app.Redis == nil {
		return "", gerror.New("Gemini 凭据刷新需要会话缓存，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	// 仅对同一凭据串行换票，网络/数据库操作的预算短于锁 TTL。锁自然到期，
	// 避免在临界点以非原子的 GET/DEL 错删其他节点取得的锁。
	key := fmt.Sprintf("aiferry:gemini-refresh:%d:%d", channelID, credentialID)
	locked, err := s.app.Redis.SetNX(ctx, key, "1", 35*time.Second).Result()
	if err != nil {
		return "", gerror.New("锁定 Gemini 凭据刷新失败")
	}
	if !locked {
		return s.waitGeminiRefresh(ctx, cfg, channelID, credentialID)
	}
	row, current, err := s.readGeminiCredential(ctx, cfg, channelID, credentialID)
	if err != nil {
		return "", err
	}
	if geminiCredentialFresh(current) {
		return current.AccessToken, nil
	}
	var channel entity.Channels
	if err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID, Type: GeminiSubscriptionChannelType}).Scan(&channel); err != nil || channel.Id == 0 {
		return "", gerror.New("Gemini 凭据所属订阅渠道不存在")
	}
	client, err := s.GeminiSubscriptionHTTPClient(ctx, channel, credentialID)
	if err != nil {
		return "", err
	}
	tokens, err := exchangeGeminiOAuthTokens(ctx, client, cfg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {current.RefreshToken}})
	if err != nil {
		return "", err
	}
	current = mergeGeminiRefresh(current, tokens, time.Now())
	sealed, err := cfg.seal(geminiCredentialAAD(channelID, row.KeyHash), current)
	if err != nil {
		return "", err
	}
	// 禁用、删除或重新授权都不能被在途刷新覆盖。
	updated, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credentialID, ChannelId: channelID, Status: 1, ApiKeyCipher: row.ApiKeyCipher}).Data(do.ChannelCredentials{ApiKeyCipher: geminiCredentialPrefix + sealed}).Update()
	if err != nil {
		return "", gerror.New("保存 Gemini 刷新凭据失败，请重新登录")
	}
	if n, err := updated.RowsAffected(); err != nil || n != 1 {
		return "", gerror.New("Gemini 凭据已变更，请重试请求")
	}
	s.invalidateCredentialCache(ctx)
	_ = s.invalidateRoutes(ctx)
	return current.AccessToken, nil
}

func mergeGeminiRefresh(current geminiSubscriptionCredential, tokens geminiOAuthTokens, now time.Time) geminiSubscriptionCredential {
	current.AccessToken = tokens.AccessToken
	current.ExpiresAt = now.Add(time.Duration(tokens.ExpiresIn) * time.Second).Unix()
	if tokens.RefreshToken != "" {
		current.RefreshToken = tokens.RefreshToken
	}
	return current
}

func (s *sChannel) waitGeminiRefresh(ctx context.Context, cfg geminiOAuthConfig, channelID, credentialID uint64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", gerror.New("Gemini 凭据正在刷新，请稍后重试")
		case <-ticker.C:
			_, current, err := s.readGeminiCredential(ctx, cfg, channelID, credentialID)
			if err != nil {
				return "", err
			}
			if geminiCredentialFresh(current) {
				return current.AccessToken, nil
			}
		}
	}
}
