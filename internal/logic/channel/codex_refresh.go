package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func exchangeCodexTokens(ctx context.Context, client *http.Client, form url.Values) (codexTokenResponse, error) {
	var tokens codexTokenResponse
	status, err := codexAuthRequest(ctx, client, "/oauth/token", "application/x-www-form-urlencoded", form.Encode(), &tokens)
	if err != nil {
		return tokens, err
	}
	if status < 200 || status >= 300 {
		return tokens, gerror.Newf("Codex 授权换票失败（HTTP %d），请重新登录", status)
	}
	return tokens, nil
}

func (s *sChannel) codexCredentialForRequest(ctx context.Context, original codexCredential) (codexCredential, error) {
	if original.ExpiresAt > time.Now().Add(time.Minute).Unix() {
		return original, nil
	}
	if s.app.Redis == nil {
		return codexCredential{}, gerror.New("Codex 凭据刷新需要可用的会话缓存，请稍后重试")
	}
	// 所有节点按账号互斥刷新，防止轮转型 refresh_token 被并发消费。
	key := "aiferry:codex-refresh:" + codexAccountHash(original.AccountID) + ":" + fmt.Sprint(original.ChannelID)
	nonce, err := randomCodexState()
	if err != nil {
		return codexCredential{}, err
	}
	locked, err := s.app.Redis.SetNX(ctx, key, nonce, 50*time.Second).Result()
	if err != nil {
		return codexCredential{}, gerror.New("锁定 Codex 凭据刷新失败")
	}
	if !locked {
		waitCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-waitCtx.Done():
				return codexCredential{}, gerror.New("Codex 凭据正在刷新，请稍后重试")
			case <-ticker.C:
				_, current, e := s.latestCodexCredential(ctx, original.ChannelID, original.AccountID)
				if e != nil {
					return codexCredential{}, e
				}
				if current.ExpiresAt > time.Now().Add(time.Minute).Unix() {
					return current, nil
				}
			}
		}
	}
	// 失败时保留短锁避免打爆授权服务；成功后也保留锁到自然到期，避免非原子
	// GET+DEL 误删其他节点在边界时刻获得的新锁。新令牌不需要再走刷新锁。
	row, current, err := s.latestCodexCredential(ctx, original.ChannelID, original.AccountID)
	if err != nil {
		return codexCredential{}, err
	}
	if current.ExpiresAt > time.Now().Add(time.Minute).Unix() {
		return current, nil
	}
	var channel entity.Channels
	if err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: row.ChannelId}).Scan(&channel); err != nil || channel.Id == 0 {
		return codexCredential{}, gerror.New("Codex 凭据所属渠道不存在")
	}
	client, err := s.HTTPClientForChannel(ctx, channel)
	if err != nil {
		return codexCredential{}, err
	}
	body, _ := json.Marshal(map[string]string{"grant_type": "refresh_token", "client_id": codexClientID, "refresh_token": current.RefreshToken, "refresh_reason": "proactive_expiry"})
	var tokens codexTokenResponse
	status, err := codexAuthRequest(ctx, client, "/oauth/token", "application/json", string(body), &tokens)
	if err != nil {
		return codexCredential{}, err
	}
	if status == 400 || status == 401 || status == 403 {
		return codexCredential{}, gerror.New("Codex 登录授权已失效，请重新登录")
	}
	if status < 200 || status >= 300 {
		return codexCredential{}, gerror.Newf("Codex 凭据刷新失败（HTTP %d），请稍后重试", status)
	}
	next, err := mergeCodexTokenRefresh(current, tokens)
	if err != nil {
		return codexCredential{}, err
	}
	cipher, err := s.app.Secrets.Encrypt(encodeCodexCredential(next))
	if err != nil {
		return codexCredential{}, err
	}
	// 当前账号可能被多个渠道登录复用，按旧密文进行 CAS 防止覆盖同时重新登录的凭据。
	result, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: row.Id, ApiKeyCipher: row.ApiKeyCipher}).Data(do.ChannelCredentials{ApiKeyCipher: cipher}).Update()
	if err != nil {
		return codexCredential{}, gerror.New("保存 Codex 刷新凭据失败，请重新登录")
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return codexCredential{}, gerror.New("Codex 凭据已变更，请重试请求")
	}
	s.invalidateCredentialCache(ctx)
	if err = s.invalidateRoutes(ctx); err != nil {
		return codexCredential{}, err
	}
	return next, nil
}

func (s *sChannel) latestCodexCredential(ctx context.Context, channelID uint64, accountID string) (credentialRow, codexCredential, error) {
	var row credentialRow
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID, KeyHash: codexAccountHash(accountID)}).OrderDesc(dao.ChannelCredentials.Columns().Id).Scan(&row); err != nil || row.Id == 0 {
		return row, codexCredential{}, gerror.New("Codex 登录凭据不存在，请重新授权")
	}
	plain, err := s.app.Secrets.Decrypt(row.ApiKeyCipher)
	if err != nil {
		return row, codexCredential{}, gerror.New("读取 Codex 登录凭据失败")
	}
	credential, err := decodeCodexCredential(plain)
	return row, credential, err
}

func mergeCodexTokenRefresh(current codexCredential, tokens codexTokenResponse) (codexCredential, error) {
	if tokens.IDToken != "" {
		account, err := codexTokenAccount(tokens.IDToken)
		if err != nil || account != current.AccountID {
			return codexCredential{}, gerror.New("Codex 刷新返回了不匹配的套餐账号，请重新登录")
		}
	}
	if tokens.AccessToken == "" {
		return codexCredential{}, gerror.New("Codex 刷新未返回访问令牌，请重新登录")
	}
	expiry, err := codexAccessExpiry(tokens.AccessToken, tokens.ExpiresIn)
	if err != nil {
		return codexCredential{}, err
	}
	current.AccessToken = tokens.AccessToken
	current.ExpiresAt = expiry
	if tokens.RefreshToken != "" {
		current.RefreshToken = tokens.RefreshToken
	}
	return current, nil
}
