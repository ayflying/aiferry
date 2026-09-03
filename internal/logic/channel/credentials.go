package channel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

type CredentialView struct {
	Id                     uint64     `json:"id"`
	KeyPrefix              string     `json:"keyPrefix"`
	Status                 int        `json:"status"`
	AutoDisabled           bool       `json:"autoDisabled"`
	AutoDisabledAt         *time.Time `json:"autoDisabledAt"`
	AutoDisabledReason     string     `json:"autoDisabledReason"`
	AutoDisabledStatusCode *uint      `json:"autoDisabledStatusCode"`
	LastCostUsed           *float64   `json:"lastCostUsed"`
	LastCostRemaining      *float64   `json:"lastCostRemaining"`
	LastCostCurrency       string     `json:"lastCostCurrency"`
	LastCostAt             *time.Time `json:"lastCostAt"`
	LastCostUsage          *float64   `json:"lastCostUsage,omitempty"`
	LastCostUsageUnit      string     `json:"lastCostUsageUnit,omitempty"`
	LastCostUsageType      string     `json:"lastCostUsageType,omitempty"`
	LastCostUsageDimension string     `json:"lastCostUsageDimension,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
}

type RouteCredential struct {
	ID           uint64
	APIKeyCipher string
}

type credentialRow struct {
	Id                     uint64     `orm:"id"`
	ChannelId              uint64     `orm:"channel_id"`
	KeyPrefix              string     `orm:"key_prefix"`
	KeyHash                string     `orm:"key_hash"`
	ApiKeyCipher           string     `orm:"api_key_cipher"`
	Status                 int        `orm:"status"`
	AutoDisabledAt         *time.Time `orm:"auto_disabled_at"`
	AutoDisabledReason     string     `orm:"auto_disabled_reason"`
	AutoDisabledStatusCode *uint      `orm:"auto_disabled_status_code"`
	AutoDisabledSource     string     `orm:"auto_disabled_source"`
	LastCostUsed           *float64   `orm:"last_cost_used"`
	LastCostRemaining      *float64   `orm:"last_cost_remaining"`
	LastCostCurrency       string     `orm:"last_cost_currency"`
	LastCostAt             *time.Time `orm:"last_cost_at"`
	CreatedAt              time.Time  `orm:"created_at"`
}

func (s *sChannel) CreateCredential(ctx context.Context, channelID uint64, input adminapi.ChannelCredentialInput) (uint64, error) {
	if _, err := s.Get(ctx, channelID); err != nil {
		return 0, err
	}
	plainText := strings.TrimSpace(input.APIKey)
	keyHash := upstreamKeyHash(plainText)
	data, err := s.newCredentialData(plainText)
	if err != nil {
		return 0, err
	}
	exists, err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID, KeyHash: keyHash}).Count()
	if err != nil {
		return 0, gerror.Wrap(err, "check duplicate channel credential")
	}
	if exists > 0 {
		return 0, gerror.New("该渠道已添加相同密钥")
	}
	data.ChannelId = channelID
	id, err := dao.ChannelCredentials.Ctx(ctx).Data(data).InsertAndGetId()
	if err != nil {
		return 0, gerror.Wrap(err, "create channel credential")
	}
	s.InvalidateListCache(ctx)
	return uint64(id), s.invalidateRoutes(ctx)
}

func (s *sChannel) createCredentialTx(ctx context.Context, channelID uint64, value string) error {
	data, err := s.newCredentialData(value)
	if err != nil {
		return err
	}
	data.ChannelId = channelID
	if _, err = dao.ChannelCredentials.Ctx(ctx).Data(data).Insert(); err != nil {
		return gerror.Wrap(err, "create initial channel credential")
	}
	s.invalidateCredentialCache(ctx)
	return nil
}

func (s *sChannel) ListCredentials(ctx context.Context, channelID uint64) ([]CredentialView, error) {
	channel, err := s.Get(ctx, channelID)
	if err != nil {
		return nil, err
	}
	_, channelTypeConfig, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return nil, err
	}
	rows := make([]credentialRow, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID}).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list channel credentials")
	}
	views := make([]CredentialView, 0, len(rows))
	for _, row := range rows {
		if err := s.ensureCredentialMetadata(ctx, &row); err != nil {
			return nil, err
		}
		views = append(views, credentialView(row, channelTypeConfig.Costs))
	}
	return views, nil
}

func (s *sChannel) SetCredentialStatus(ctx context.Context, channelID, credentialID uint64, input adminapi.ChannelCredentialStatusInput) error {
	credential, err := s.credentialByID(ctx, channelID, credentialID)
	if err != nil {
		return err
	}
	data := do.ChannelCredentials{
		Status:                 boolStatus(input.Status),
		AutoDisabledAt:         gdb.Raw("NULL"),
		AutoDisabledReason:     gdb.Raw("NULL"),
		AutoDisabledStatusCode: gdb.Raw("NULL"),
		AutoDisabledSource:     gdb.Raw("NULL"),
	}
	if _, err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credential.Id}).Data(data).Update(); err != nil {
		return gerror.Wrap(err, "update channel credential status")
	}
	s.clearCredentialTransient(ctx, credential.Id)
	s.resilience.ResetCredentialRecoverySchedule(ctx, credential.Id)
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}

func (s *sChannel) DeleteCredential(ctx context.Context, channelID, credentialID uint64) error {	credential, err := s.credentialByID(ctx, channelID, credentialID)
	if err != nil {
		return err
	}
	if err = dao.ChannelCredentials.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		if _, deleteErr := dao.ApiKeyChannelCredentials.Ctx(txCtx).Where(do.ApiKeyChannelCredentials{ChannelCredentialId: credential.Id}).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "remove channel credential bindings")
		}
		if _, deleteErr := dao.ChannelCredentials.Ctx(txCtx).Where(do.ChannelCredentials{Id: credential.Id}).Delete(); deleteErr != nil {
			return gerror.Wrap(deleteErr, "delete channel credential")
		}
		return nil
	}); err != nil {
		return err
	}
	s.clearCredentialTransient(ctx, credential.Id)
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}

func (s *sChannel) HasAvailableCredential(ctx context.Context, channelID uint64) (bool, error) {
	credentials, err := s.availableCredentials(ctx, channelID, nil)
	return len(credentials) > 0, err
}

func isMissingCredentialBindingError(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// 凭证绑定缓存：SelectCredential 首次尝试（excluded==nil）会查询
// api_key_channel_credentials 确定密钥的固定绑定凭证。绑定关系本身只在
// 首次使用或重试切换时变化，属低频写，用 60 秒 TTL 缓存消除热路径查询。
// 绑定写入路径（bindFirstCredential/replaceCredentialBinding）会主动失效。
const credentialBindingCacheTTL = 60 * time.Second

func credentialBindingCacheKey(apiKeyID, channelID uint64) string {
	return fmt.Sprintf("aiferry:credential-binding:%d:%d", apiKeyID, channelID)
}

func (s *sChannel) readCredentialBindingCache(ctx context.Context, apiKeyID, channelID uint64) (entity.ApiKeyChannelCredentials, bool) {
	encoded, err := s.app.Redis.Get(ctx, credentialBindingCacheKey(apiKeyID, channelID)).Bytes()
	if err != nil {
		return entity.ApiKeyChannelCredentials{}, false
	}
	var binding entity.ApiKeyChannelCredentials
	if err := json.Unmarshal(encoded, &binding); err != nil || binding.ApiKeyId != apiKeyID {
		_ = s.app.Redis.Del(ctx, credentialBindingCacheKey(apiKeyID, channelID)).Err()
		return entity.ApiKeyChannelCredentials{}, false
	}
	return binding, true
}

func (s *sChannel) writeCredentialBindingCache(ctx context.Context, apiKeyID, channelID uint64, binding entity.ApiKeyChannelCredentials) {
	encoded, err := json.Marshal(binding)
	if err != nil {
		return
	}
	_ = s.app.Redis.Set(ctx, credentialBindingCacheKey(apiKeyID, channelID), encoded, credentialBindingCacheTTL).Err()
}

func (s *sChannel) invalidateCredentialBinding(ctx context.Context, apiKeyID, channelID uint64) {
	_ = s.app.Redis.Del(ctx, credentialBindingCacheKey(apiKeyID, channelID)).Err()
}

func (s *sChannel) SelectCredential(ctx context.Context, apiKeyID, channelID uint64, excluded map[uint64]struct{}) (RouteCredential, error) {
	credentials, err := s.availableCredentials(ctx, channelID, excluded)
	if err != nil {
		return RouteCredential{}, err
	}
	if len(credentials) == 0 {
		return RouteCredential{}, gerror.New("channel has no available upstream credential")
	}
	var binding entity.ApiKeyChannelCredentials
	if excluded == nil {
		cached, cachedOK := s.readCredentialBindingCache(ctx, apiKeyID, channelID)
		if cachedOK {
			binding = cached
		} else {
			if err = dao.ApiKeyChannelCredentials.Ctx(ctx).Where(do.ApiKeyChannelCredentials{ApiKeyId: apiKeyID, ChannelId: channelID}).Scan(&binding); err != nil && !isMissingCredentialBindingError(err) {
				return RouteCredential{}, gerror.Wrap(err, "load channel credential binding")
			}
			if binding.ChannelCredentialId > 0 {
				s.writeCredentialBindingCache(ctx, apiKeyID, channelID, binding)
			}
		}
		if binding.ChannelCredentialId > 0 {
			for _, credential := range credentials {
				if credential.Id == binding.ChannelCredentialId {
					return RouteCredential{ID: credential.Id, APIKeyCipher: credential.ApiKeyCipher}, nil
				}
			}
		}
	}
	selected := credentials[mathrand.IntN(len(credentials))]
	if excluded == nil && binding.ChannelCredentialId == 0 {
		selected, err = s.bindFirstCredential(ctx, apiKeyID, channelID, selected, credentials)
		if err != nil {
			return RouteCredential{}, err
		}
	} else if err = s.replaceCredentialBinding(ctx, apiKeyID, channelID, selected.Id); err != nil {
		return RouteCredential{}, err
	}
	return RouteCredential{ID: selected.Id, APIKeyCipher: selected.ApiKeyCipher}, nil
}

func (s *sChannel) CredentialForTest(ctx context.Context, channelID, credentialID uint64) (RouteCredential, error) {
	if credentialID > 0 {
		credential, err := s.credentialByID(ctx, channelID, credentialID)
		if err != nil {
			return RouteCredential{}, err
		}
		return RouteCredential{ID: credential.Id, APIKeyCipher: credential.ApiKeyCipher}, nil
	}
	credentials, err := s.availableCredentials(ctx, channelID, nil)
	if err != nil {
		return RouteCredential{}, err
	}
	if len(credentials) == 0 {
		return RouteCredential{}, gerror.New("channel has no available upstream credential")
	}
	selected := credentials[mathrand.IntN(len(credentials))]
	return RouteCredential{ID: selected.Id, APIKeyCipher: selected.ApiKeyCipher}, nil
}

func (s *sChannel) clearCredentialTransient(ctx context.Context, credentialID uint64) {
	_ = s.app.Redis.Del(ctx, system.CredentialFailureKey(credentialID), system.CredentialCooldownKey(credentialID)).Err()
	s.invalidateCredentialCache(ctx)
}

// 凭证可用性缓存：channel_credentials 表的可用密钥池在稳定状态下很少变化，
// 而转发热路径每个候选渠道都会调用 availableCredentials 扫描一次数据库。
// 采用"版本号嵌入缓存键"失效策略（与路由缓存一致）：写路径递增
// aiferry:credentials:version，版本号变化后旧键自然失效；TTL 仅作兜底。
// 缓存只存 status=1 的原始凭证行，冷却排除与重试排除（excluded）在每次
// 请求时实时执行，保证禁用恢复与负载均衡行为不变。
const (
	credentialCacheVersionKey = "aiferry:credentials:version"
	credentialCacheTTL        = 5 * time.Minute
)

type credentialCacheEntry struct {
	Credentials []credentialRow `json:"credentials"`
}

func credentialCacheKey(channelID uint64, version int64) string {
	return fmt.Sprintf("aiferry:credentials:%d:%d", channelID, version)
}

func (s *sChannel) credentialCacheVersion(ctx context.Context) int64 {
	version, err := s.app.Redis.Get(ctx, credentialCacheVersionKey).Int64()
	if err != nil {
		return 0
	}
	return version
}

func (s *sChannel) invalidateCredentialCache(ctx context.Context) {
	_ = s.app.Redis.Incr(ctx, credentialCacheVersionKey).Err()
}

func (s *sChannel) readCredentialCache(ctx context.Context, channelID uint64, version int64) ([]credentialRow, bool) {
	encoded, err := s.app.Redis.Get(ctx, credentialCacheKey(channelID, version)).Bytes()
	if err != nil {
		return nil, false
	}
	var entry credentialCacheEntry
	if err := json.Unmarshal(encoded, &entry); err != nil {
		_ = s.app.Redis.Del(ctx, credentialCacheKey(channelID, version)).Err()
		return nil, false
	}
	return entry.Credentials, true
}

func (s *sChannel) writeCredentialCache(ctx context.Context, channelID uint64, version int64, rows []credentialRow) {
	if len(rows) == 0 {
		return
	}
	encoded, err := json.Marshal(credentialCacheEntry{Credentials: rows})
	if err != nil {
		return
	}
	_ = s.app.Redis.Set(ctx, credentialCacheKey(channelID, version), encoded, credentialCacheTTL).Err()
}

func (s *sChannel) availableCredentials(ctx context.Context, channelID uint64, excluded map[uint64]struct{}) ([]credentialRow, error) {
	version := s.credentialCacheVersion(ctx)
	rows, ok := s.readCredentialCache(ctx, channelID, version)
	if !ok {
		rows = make([]credentialRow, 0)
		if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID, Status: 1}).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&rows); err != nil {
			return nil, gerror.Wrap(err, "list available channel credentials")
		}
		s.writeCredentialCache(ctx, channelID, version, rows)
	}
	available := make([]credentialRow, 0, len(rows))
	for _, row := range rows {
		if _, skip := excluded[row.Id]; skip {
			continue
		}
		if cooling, _ := s.app.Redis.Exists(ctx, system.CredentialCooldownKey(row.Id)).Result(); cooling > 0 {
			continue
		}
		available = append(available, row)
	}
	return available, nil
}

func (s *sChannel) credentialByID(ctx context.Context, channelID, credentialID uint64) (credentialRow, error) {
	var row credentialRow
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credentialID, ChannelId: channelID}).Scan(&row); err != nil {
		return row, gerror.Wrap(err, "find channel credential")
	}
	if row.Id == 0 {
		return row, gerror.New("channel credential not found")
	}
	return row, nil
}

func (s *sChannel) bindFirstCredential(ctx context.Context, apiKeyID, channelID uint64, selected credentialRow, available []credentialRow) (credentialRow, error) {
	result, err := dao.ApiKeyChannelCredentials.Ctx(ctx).Data(do.ApiKeyChannelCredentials{
		ApiKeyId: apiKeyID, ChannelId: channelID, ChannelCredentialId: selected.Id,
	}).InsertIgnore()
	if err != nil {
		return credentialRow{}, gerror.Wrap(err, "create channel credential binding")
	}
	if inserted, _ := result.RowsAffected(); inserted > 0 {
		s.writeCredentialBindingCache(ctx, apiKeyID, channelID, entity.ApiKeyChannelCredentials{
			ApiKeyId: apiKeyID, ChannelId: channelID, ChannelCredentialId: selected.Id,
		})
		return selected, nil
	}

	var binding entity.ApiKeyChannelCredentials
	if err = dao.ApiKeyChannelCredentials.Ctx(ctx).Where(do.ApiKeyChannelCredentials{ApiKeyId: apiKeyID, ChannelId: channelID}).Scan(&binding); err != nil {
		return credentialRow{}, gerror.Wrap(err, "load concurrent channel credential binding")
	}
	s.writeCredentialBindingCache(ctx, apiKeyID, channelID, binding)
	for _, credential := range available {
		if credential.Id == binding.ChannelCredentialId {
			return credential, nil
		}
	}
	if err = s.replaceCredentialBinding(ctx, apiKeyID, channelID, selected.Id); err != nil {
		return credentialRow{}, err
	}
	return selected, nil
}

func (s *sChannel) replaceCredentialBinding(ctx context.Context, apiKeyID, channelID, credentialID uint64) error {
	if _, err := dao.ApiKeyChannelCredentials.Ctx(ctx).
		Where(do.ApiKeyChannelCredentials{ApiKeyId: apiKeyID, ChannelId: channelID}).
		Data(do.ApiKeyChannelCredentials{ChannelCredentialId: credentialID}).
		Update(); err != nil {
		return gerror.Wrap(err, "update channel credential binding")
	}
	s.writeCredentialBindingCache(ctx, apiKeyID, channelID, entity.ApiKeyChannelCredentials{
		ApiKeyId: apiKeyID, ChannelId: channelID, ChannelCredentialId: credentialID,
	})
	return nil
}

func (s *sChannel) ensureCredentialMetadata(ctx context.Context, row *credentialRow) error {
	if row.KeyPrefix != "" && row.KeyHash != "" {
		return nil
	}
	plainText, err := s.app.Secrets.Decrypt(row.ApiKeyCipher)
	if err != nil {
		return gerror.Wrap(err, "decrypt channel credential")
	}
	row.KeyPrefix = maskedCredentialPrefix(plainText)
	row.KeyHash = upstreamKeyHash(plainText)
	if _, err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: row.Id}).Data(do.ChannelCredentials{KeyPrefix: row.KeyPrefix, KeyHash: row.KeyHash}).Update(); err != nil {
		return gerror.Wrap(err, "complete channel credential metadata")
	}
	return nil
}

func (s *sChannel) newCredentialData(value string) (do.ChannelCredentials, error) {
	cipherText, err := s.app.Secrets.Encrypt(strings.TrimSpace(value))
	if err != nil {
		return do.ChannelCredentials{}, err
	}
	return do.ChannelCredentials{
		KeyPrefix: maskedCredentialPrefix(value), KeyHash: upstreamKeyHash(value), ApiKeyCipher: cipherText, Status: 1,
	}, nil
}

func credentialView(row credentialRow, config channeltype.CostConfig) CredentialView {
	view := CredentialView{
		Id: row.Id, KeyPrefix: row.KeyPrefix, Status: row.Status, AutoDisabledReason: row.AutoDisabledReason,
		AutoDisabledStatusCode: row.AutoDisabledStatusCode, LastCostUsed: row.LastCostUsed, LastCostRemaining: row.LastCostRemaining,
		LastCostCurrency: row.LastCostCurrency, LastCostAt: row.LastCostAt, CreatedAt: row.CreatedAt,
	}
	if row.AutoDisabledAt != nil {
		view.AutoDisabled = true
		view.AutoDisabledAt = row.AutoDisabledAt
	}
	if channeltype.IsUsageCost(config) && row.LastCostUsed != nil {
		view.LastCostUsage = row.LastCostUsed
		view.LastCostUsageUnit = config.UsageUnit
		if view.LastCostUsageUnit == "" {
			view.LastCostUsageUnit = "kToken"
		}
		view.LastCostUsageType = config.UsageType
		if view.LastCostUsageType == "" {
			view.LastCostUsageType = "用量"
		}
		view.LastCostUsageDimension = config.UsageDimension
	}
	return view
}

func maskedCredentialPrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "无密钥"
	}
	if len(value) <= 8 {
		return "已配置"
	}
	return value[:8] + "..."
}

func upstreamKeyHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
