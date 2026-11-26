package channel

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
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
	ProjectID    string `json:"ProjectID,omitempty"`
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

// New project identifiers are required at account creation; legacy project
// identifiers continue working as stored (including existing project numbers).
var geminiProjectIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

func geminiCredentialProject(credential geminiSubscriptionCredential, legacyProject string) string {
	if project := strings.TrimSpace(credential.ProjectID); project != "" {
		return project
	}
	return strings.TrimSpace(legacyProject)
}

func (s *sChannel) saveGeminiCredential(ctx context.Context, cfg geminiOAuthConfig, channelID, targetID uint64, credential geminiSubscriptionCredential) (uint64, error) {
	var id uint64
	err := dao.Channels.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		var channel entity.Channels
		model := dao.Channels.Ctx(txCtx).Where(do.Channels{Id: channelID, Type: GeminiSubscriptionChannelType})
		if dao.Channels.DB().GetConfig().Type != "sqlite" {
			model = model.LockUpdate()
		}
		if err := model.Scan(&channel); err != nil || channel.Id == 0 || !channel.DeletedAt.IsZero() {
			return gerror.New("Gemini 凭据所属订阅渠道不存在")
		}
		var row credentialRow
		if targetID == 0 {
			if !geminiProjectIDPattern.MatchString(credential.ProjectID) {
				return gerror.New("新增 Gemini 账号需要有效的项目 ID")
			}
		} else {
			currentRow, current, err := s.readGeminiLoginTarget(txCtx, cfg, channelID, targetID)
			if err != nil {
				return err
			}
			if current.Subject != credential.Subject {
				return gerror.New("Google 授权账号与原凭据不一致，原凭据未更改")
			}
			project := geminiCredentialProject(current, channel.ProjectId)
			if project != credential.ProjectID {
				return gerror.New("原凭据项目已变更，请重新发起授权")
			}
			row = currentRow
		}
		hash := geminiAccountHash(credential.Subject)
		sealed, err := cfg.seal(geminiCredentialAAD(channelID, hash), credential)
		if err != nil {
			return err
		}
		cipher := geminiCredentialPrefix + sealed
		if targetID != 0 {
			id = row.Id
			return replaceLoginCredential(txCtx, row, cipher)
		}
		id, err = insertLoginCredential(txCtx, channelID, hash, "Gemini OAuth "+maskedCredentialPrefix(hash), cipher)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.invalidateLoginCredentials(ctx)
	return id, nil
}

func decodeGeminiCredential(cfg geminiOAuthConfig, row credentialRow) (geminiSubscriptionCredential, error) {
	var credential geminiSubscriptionCredential
	if !strings.HasPrefix(row.ApiKeyCipher, geminiCredentialPrefix) {
		return credential, gerror.New("Gemini 登录凭据无效，请重新授权")
	}
	if err := cfg.open(geminiCredentialAAD(row.ChannelId, row.KeyHash), strings.TrimPrefix(row.ApiKeyCipher, geminiCredentialPrefix), &credential); err != nil {
		return credential, err
	}
	if credential.Subject == "" || row.KeyHash != geminiAccountHash(credential.Subject) || !validGeminiOAuthToken(credential.AccessToken) || !validGeminiOAuthToken(credential.RefreshToken) || credential.ExpiresAt <= 0 {
		return credential, gerror.New("Gemini 登录凭据无效，请重新授权")
	}
	return credential, nil
}

func (s *sChannel) readGeminiLoginTarget(ctx context.Context, cfg geminiOAuthConfig, channelID, credentialID uint64) (credentialRow, geminiSubscriptionCredential, error) {
	row, err := s.credentialByID(ctx, channelID, credentialID)
	if err != nil {
		return row, geminiSubscriptionCredential{}, gerror.New("Gemini 登录凭据不存在")
	}
	credential, err := decodeGeminiCredential(cfg, row)
	return row, credential, err
}

func (s *sChannel) readGeminiCredential(ctx context.Context, cfg geminiOAuthConfig, channelID, credentialID uint64) (credentialRow, geminiSubscriptionCredential, error) {
	row, credential, err := s.readGeminiLoginTarget(ctx, cfg, channelID, credentialID)
	if err != nil || row.Status != 1 {
		return row, geminiSubscriptionCredential{}, gerror.New("Gemini 登录凭据不存在、未启用或无效，请检查渠道凭据")
	}
	return row, credential, nil
}

// GeminiSubscriptionAuthorization returns the token and project from the same
// credential version. legacyProject is used only when an older credential has
// no project in its sealed payload. Use only with the official Gemini endpoint.
func (s *sChannel) GeminiSubscriptionAuthorization(ctx context.Context, channelID, credentialID uint64, cipher, legacyProject string) (token, project string, err error) {
	credential, err := s.geminiAuthorizationCredential(ctx, channelID, credentialID, cipher)
	if err != nil {
		return "", "", err
	}
	project = geminiCredentialProject(credential, legacyProject)
	if project == "" {
		return "", "", gerror.New("Gemini 订阅账号未配置项目 ID")
	}
	return credential.AccessToken, project, nil
}

// GeminiSubscriptionAccessToken resolves the credential for callers which do
// not construct a project-scoped request (including old integration callers).
func (s *sChannel) GeminiSubscriptionAccessToken(ctx context.Context, channelID, credentialID uint64, cipher string) (string, error) {
	credential, err := s.geminiAuthorizationCredential(ctx, channelID, credentialID, cipher)
	return credential.AccessToken, err
}

func (s *sChannel) geminiAuthorizationCredential(ctx context.Context, channelID, credentialID uint64, cipher string) (geminiSubscriptionCredential, error) {
	if channelID == 0 || credentialID == 0 || !strings.HasPrefix(cipher, geminiCredentialPrefix) {
		return geminiSubscriptionCredential{}, gerror.New("Gemini 订阅渠道需要通过 Google OAuth 登录授权")
	}
	cfg, err := readGeminiOAuthConfig()
	if err != nil {
		return geminiSubscriptionCredential{}, err
	}
	_, credential, err := s.readGeminiCredential(ctx, cfg, channelID, credentialID)
	if err != nil || geminiCredentialFresh(credential) {
		return credential, err
	}
	return s.refreshGeminiCredential(ctx, cfg, channelID, credentialID)
}

func geminiCredentialFresh(credential geminiSubscriptionCredential) bool {
	return credential.ExpiresAt > time.Now().Add(geminiRefreshAhead).Unix()
}

func (s *sChannel) refreshGeminiCredential(ctx context.Context, cfg geminiOAuthConfig, channelID, credentialID uint64) (geminiSubscriptionCredential, error) {
	if s.app.Redis == nil {
		return geminiSubscriptionCredential{}, gerror.New("Gemini 凭据刷新需要会话缓存，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	// 仅对同一凭据串行换票，网络/数据库操作的预算短于锁 TTL。锁自然到期，
	// 避免在临界点以非原子的 GET/DEL 错删其他节点取得的锁。
	key := fmt.Sprintf("aiferry:gemini-refresh:%d:%d", channelID, credentialID)
	locked, err := s.app.Redis.SetNX(ctx, key, "1", 35*time.Second).Result()
	if err != nil {
		return geminiSubscriptionCredential{}, gerror.New("锁定 Gemini 凭据刷新失败")
	}
	if !locked {
		return s.waitGeminiRefresh(ctx, cfg, channelID, credentialID)
	}
	row, current, err := s.readGeminiCredential(ctx, cfg, channelID, credentialID)
	if err != nil {
		return geminiSubscriptionCredential{}, err
	}
	if geminiCredentialFresh(current) {
		return current, nil
	}
	var channel entity.Channels
	if err = dao.Channels.Ctx(ctx).Where(do.Channels{Id: channelID, Type: GeminiSubscriptionChannelType}).Scan(&channel); err != nil || channel.Id == 0 {
		return geminiSubscriptionCredential{}, gerror.New("Gemini 凭据所属订阅渠道不存在")
	}
	client, err := s.GeminiSubscriptionHTTPClient(ctx, channel, credentialID)
	if err != nil {
		return geminiSubscriptionCredential{}, err
	}
	tokens, err := exchangeGeminiOAuthTokens(ctx, client, cfg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {current.RefreshToken}})
	if err != nil {
		return geminiSubscriptionCredential{}, err
	}
	current = mergeGeminiRefresh(current, tokens, time.Now())
	sealed, err := cfg.seal(geminiCredentialAAD(channelID, row.KeyHash), current)
	if err != nil {
		return geminiSubscriptionCredential{}, err
	}
	// 禁用、删除或重新授权都不能被在途刷新覆盖。
	updated, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credentialID, ChannelId: channelID, Status: 1, ApiKeyCipher: row.ApiKeyCipher}).Data(do.ChannelCredentials{ApiKeyCipher: geminiCredentialPrefix + sealed}).Update()
	if err != nil {
		return geminiSubscriptionCredential{}, gerror.New("保存 Gemini 刷新凭据失败，请重新登录")
	}
	if n, err := updated.RowsAffected(); err != nil || n != 1 {
		return geminiSubscriptionCredential{}, gerror.New("Gemini 凭据已变更，请重试请求")
	}
	s.invalidateCredentialCache(ctx)
	_ = s.invalidateRoutes(ctx)
	return current, nil
}

func mergeGeminiRefresh(current geminiSubscriptionCredential, tokens geminiOAuthTokens, now time.Time) geminiSubscriptionCredential {
	current.AccessToken = tokens.AccessToken
	current.ExpiresAt = now.Add(time.Duration(tokens.ExpiresIn) * time.Second).Unix()
	if tokens.RefreshToken != "" {
		current.RefreshToken = tokens.RefreshToken
	}
	return current
}

func (s *sChannel) waitGeminiRefresh(ctx context.Context, cfg geminiOAuthConfig, channelID, credentialID uint64) (geminiSubscriptionCredential, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return geminiSubscriptionCredential{}, gerror.New("Gemini 凭据正在刷新，请稍后重试")
		case <-ticker.C:
			_, current, err := s.readGeminiCredential(ctx, cfg, channelID, credentialID)
			if err != nil {
				return geminiSubscriptionCredential{}, err
			}
			if geminiCredentialFresh(current) {
				return current, nil
			}
		}
	}
}
