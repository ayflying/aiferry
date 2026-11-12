package channel

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

const codexCredentialPrefix = "aiferry:codex:v1:"

// OAuth 凭据沿用现有加密列，不新增明文 token 列；刷新后稳定账号哈希不变。
// JWT 仅解析官方 token 响应的账号元数据和过期时间，不用于本系统权限认证。
type codexCredential struct {
	Seal         string `json:"seal"`
	ChannelID    uint64 `json:"channelId"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	AccountID    string `json:"accountId"`
	ExpiresAt    int64  `json:"expiresAt"`
}

type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func codexJWTClaims(token string) (map[string]json.RawMessage, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, gerror.New("Codex 官方令牌格式无效")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, gerror.New("Codex 官方令牌格式无效")
	}
	var claims map[string]json.RawMessage
	if json.Unmarshal(body, &claims) != nil {
		return nil, gerror.New("Codex 官方令牌声明无效")
	}
	return claims, nil
}

func codexTokenAccount(token string) (string, error) {
	claims, err := codexJWTClaims(token)
	if err != nil {
		return "", err
	}
	var auth struct {
		AccountID string `json:"chatgpt_account_id"`
	}
	if json.Unmarshal(claims["https://api.openai.com/auth"], &auth) != nil || strings.TrimSpace(auth.AccountID) == "" {
		return "", gerror.New("Codex 登录未返回 ChatGPT 套餐账号，请使用 ChatGPT 账号授权")
	}
	return auth.AccountID, nil
}

func codexAccessExpiry(token string, expiresIn int64) (int64, error) {
	if expiresIn > 0 {
		return time.Now().Unix() + expiresIn, nil
	}
	claims, err := codexJWTClaims(token)
	if err != nil {
		return 0, err
	}
	var expires int64
	if json.Unmarshal(claims["exp"], &expires) != nil || expires <= 0 {
		return 0, gerror.New("Codex 访问令牌缺少有效期")
	}
	return expires, nil
}

func newCodexCredential(tokens codexTokenResponse) (codexCredential, error) {
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" {
		return codexCredential{}, gerror.New("Codex 授权未返回完整令牌")
	}
	account, err := codexTokenAccount(tokens.IDToken)
	if err != nil {
		return codexCredential{}, err
	}
	expiry, err := codexAccessExpiry(tokens.AccessToken, tokens.ExpiresIn)
	if err != nil {
		return codexCredential{}, err
	}
	return codexCredential{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, AccountID: account, ExpiresAt: expiry}, nil
}

func encodeCodexCredential(credential codexCredential) string {
	body, _ := json.Marshal(credential)
	return codexCredentialPrefix + string(body)
}

func decodeCodexCredential(plain string) (codexCredential, error) {
	var credential codexCredential
	if !strings.HasPrefix(plain, codexCredentialPrefix) || json.Unmarshal([]byte(strings.TrimPrefix(plain, codexCredentialPrefix)), &credential) != nil || credential.AccessToken == "" || credential.AccountID == "" || credential.ExpiresAt <= 0 {
		return credential, gerror.New("Codex 登录凭据无效，请重新授权")
	}
	return credential, nil
}

func codexAccountHash(accountID string) string { return upstreamKeyHash("codex-account:" + accountID) }

func (s *sChannel) createCodexCredential(ctx context.Context, channelID uint64, credential codexCredential) (uint64, error) {
	// 登录已在用户拥有的渠道上下文中进行；同账号再次登录更新旧凭据，避免一份套餐
	// 因多次登录被查询合并多次，也允许失效刷新令牌通过重新授权恢复。
	credential.ChannelID = channelID
	seal, err := s.app.Secrets.Encrypt(fmt.Sprintf("codex:%d:%s", channelID, credential.AccountID))
	if err != nil {
		return 0, err
	}
	credential.Seal = seal
	hash := codexAccountHash(credential.AccountID)
	var row credentialRow
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID, KeyHash: hash}).Scan(&row); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, gerror.Wrap(err, "读取 Codex 账号凭据失败")
	}
	cipher, err := s.app.Secrets.Encrypt(encodeCodexCredential(credential))
	if err != nil {
		return 0, err
	}
	data := do.ChannelCredentials{ApiKeyCipher: cipher, KeyHash: hash, KeyPrefix: "Codex " + maskedCredentialPrefix(credential.AccountID), Status: 1}
	id := row.Id
	if id == 0 {
		data.ChannelId = channelID
		inserted, e := dao.ChannelCredentials.Ctx(ctx).Data(data).InsertAndGetId()
		if e != nil {
			return 0, gerror.New("保存 Codex 登录凭据失败，请重试")
		}
		id = uint64(inserted)
	} else {
		if _, err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: id}).Data(data).Update(); err != nil {
			return 0, gerror.New("更新 Codex 登录凭据失败")
		}
	}
	s.invalidateCredentialCache(ctx)
	s.InvalidateListCache(ctx)
	// 凭据已持久化后不再把路由缓存清理故障伪装成登录失败；登录结果必须可幂等返回。
	_ = s.invalidateRoutes(ctx)
	return id, nil
}
