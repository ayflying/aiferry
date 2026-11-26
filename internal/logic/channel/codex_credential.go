package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
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
	Subject      string `json:"subject,omitempty"`
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
	return codexCredential{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, AccountID: account, Subject: codexTokenSubject(tokens.IDToken), ExpiresAt: expiry}, nil
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

// Subject and account ID are independently checked on reauthorization: a
// shared ChatGPT workspace does not establish the identity of its member.
func codexTokenSubject(token string) string {
	claims, err := codexJWTClaims(token)
	if err != nil {
		return ""
	}
	var subject string
	if json.Unmarshal(claims["sub"], &subject) != nil {
		return ""
	}
	return strings.TrimSpace(subject)
}

func (s *sChannel) decodeCodexLoginTarget(row credentialRow) (codexCredential, error) {
	plain, err := s.app.Secrets.Decrypt(row.ApiKeyCipher)
	if err != nil {
		return codexCredential{}, gerror.New("读取 Codex 登录凭据失败")
	}
	credential, err := decodeCodexCredential(plain)
	if err != nil || credential.ChannelID != row.ChannelId || codexAccountHash(credential.AccountID) != row.KeyHash || credential.Seal == "" || credential.RefreshToken == "" {
		return codexCredential{}, gerror.New("原 Codex 凭据缺少可核实的登录身份")
	}
	seal, err := s.app.Secrets.Decrypt(credential.Seal)
	if err != nil || seal != fmt.Sprintf("codex:%d:%s", row.ChannelId, credential.AccountID) {
		return codexCredential{}, gerror.New("原 Codex 凭据登录身份无效")
	}
	if credential.Subject == "" {
		credential.Subject = codexTokenSubject(credential.AccessToken)
	}
	if credential.Subject == "" {
		return codexCredential{}, gerror.New("原 Codex 凭据缺少可核实的登录身份")
	}
	return credential, nil
}

func (s *sChannel) readCodexLoginTarget(ctx context.Context, channelID, targetID uint64) (credentialRow, codexCredential, error) {
	row, err := s.credentialByID(ctx, channelID, targetID)
	if err != nil {
		return row, codexCredential{}, gerror.New("Codex 登录凭据不存在")
	}
	credential, err := s.decodeCodexLoginTarget(row)
	return row, credential, err
}

func (s *sChannel) saveCodexCredential(ctx context.Context, channelID, targetID uint64, credential codexCredential) (uint64, error) {
	var row credentialRow
	if targetID != 0 {
		currentRow, current, err := s.readCodexLoginTarget(ctx, channelID, targetID)
		if err != nil {
			return 0, err
		}
		if current.AccountID != credential.AccountID || current.Subject != credential.Subject {
			return 0, gerror.New("Codex 授权账号与原凭据不一致，原凭据未更改")
		}
		row = currentRow
	}
	credential.ChannelID = channelID
	seal, err := s.app.Secrets.Encrypt(fmt.Sprintf("codex:%d:%s", channelID, credential.AccountID))
	if err != nil {
		return 0, err
	}
	credential.Seal = seal
	cipher, err := s.app.Secrets.Encrypt(encodeCodexCredential(credential))
	if err != nil {
		return 0, err
	}
	id := targetID
	if targetID != 0 {
		err = replaceLoginCredential(ctx, row, cipher)
	} else {
		id, err = insertLoginCredential(ctx, channelID, codexAccountHash(credential.AccountID), "Codex "+maskedCredentialPrefix(credential.AccountID), cipher)
	}
	if err != nil {
		return 0, err
	}
	s.invalidateLoginCredentials(ctx)
	return id, nil
}
