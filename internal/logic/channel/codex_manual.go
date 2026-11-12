package channel

import (
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/model/do"
)

// 手动输入只接受账号 access token，不接受 API Key 或内部凭据 JSON。
// JWT 声明仅用于上游账号路由，不作为本系统身份认证；有效性最终由官方验证。
func manualCodexCredential(value string) (codexCredential, error) {
	token := strings.TrimSpace(value)
	if strings.HasPrefix(token, codexCredentialPrefix) || strings.HasPrefix(token, "sk-") {
		return codexCredential{}, gerror.New("请填写 Codex 账号访问令牌（access token），不是 OpenAI API Key 或内部凭据")
	}
	account, err := codexTokenAccount(token)
	if err != nil {
		return codexCredential{}, err
	}
	expiry, err := codexAccessExpiry(token, 0)
	if err != nil {
		return codexCredential{}, err
	}
	if expiry <= time.Now().Unix() {
		return codexCredential{}, gerror.New("Codex 访问令牌已过期，请重新填写或登录授权")
	}
	return codexCredential{AccessToken: token, AccountID: account, ExpiresAt: expiry}, nil
}

// 手动令牌与设备登录共用服务端密封格式及账号哈希，不能导入任意内部封装。
func (s *sChannel) manualCodexCredentialData(channelID uint64, value string) (do.ChannelCredentials, error) {
	credential, err := manualCodexCredential(value)
	if err != nil {
		return do.ChannelCredentials{}, err
	}
	credential.ChannelID = channelID
	credential.Seal, err = s.app.Secrets.Encrypt(fmt.Sprintf("codex:%d:%s", channelID, credential.AccountID))
	if err != nil {
		return do.ChannelCredentials{}, err
	}
	cipher, err := s.app.Secrets.Encrypt(encodeCodexCredential(credential))
	if err != nil {
		return do.ChannelCredentials{}, err
	}
	return do.ChannelCredentials{ChannelId: channelID, ApiKeyCipher: cipher, KeyHash: codexAccountHash(credential.AccountID), KeyPrefix: "Codex " + maskedCredentialPrefix(credential.AccountID), Status: 1}, nil
}
