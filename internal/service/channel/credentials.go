package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	mathrand "math/rand/v2"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/dao"
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

func (s *Service) CreateCredential(ctx context.Context, channelID uint64, input adminapi.ChannelCredentialInput) (uint64, error) {
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

func (s *Service) createCredentialTx(ctx context.Context, channelID uint64, value string) error {
	data, err := s.newCredentialData(value)
	if err != nil {
		return err
	}
	data.ChannelId = channelID
	if _, err = dao.ChannelCredentials.Ctx(ctx).Data(data).Insert(); err != nil {
		return gerror.Wrap(err, "create initial channel credential")
	}
	return nil
}

func (s *Service) ListCredentials(ctx context.Context, channelID uint64) ([]CredentialView, error) {
	if _, err := s.Get(ctx, channelID); err != nil {
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
		views = append(views, credentialView(row))
	}
	return views, nil
}

func (s *Service) SetCredentialStatus(ctx context.Context, channelID, credentialID uint64, input adminapi.ChannelCredentialStatusInput) error {
	credential, err := s.credentialByID(ctx, channelID, credentialID)
	if err != nil {
		return err
	}
	data := do.ChannelCredentials{Status: boolStatus(input.Status)}
	if input.Status == 1 {
		data.AutoDisabledAt = gdb.Raw("NULL")
		data.AutoDisabledReason = gdb.Raw("NULL")
		data.AutoDisabledStatusCode = gdb.Raw("NULL")
		data.AutoDisabledSource = gdb.Raw("NULL")
	}
	if _, err = dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credential.Id}).Data(data).Update(); err != nil {
		return gerror.Wrap(err, "update channel credential status")
	}
	s.clearCredentialTransient(ctx, credential.Id)
	s.InvalidateListCache(ctx)
	return s.invalidateRoutes(ctx)
}

func (s *Service) DeleteCredential(ctx context.Context, channelID, credentialID uint64) error {
	credential, err := s.credentialByID(ctx, channelID, credentialID)
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

func (s *Service) HasAvailableCredential(ctx context.Context, channelID uint64) (bool, error) {
	credentials, err := s.availableCredentials(ctx, channelID, nil)
	return len(credentials) > 0, err
}

func (s *Service) SelectCredential(ctx context.Context, apiKeyID, channelID uint64, excluded map[uint64]struct{}) (RouteCredential, error) {
	credentials, err := s.availableCredentials(ctx, channelID, excluded)
	if err != nil {
		return RouteCredential{}, err
	}
	if len(credentials) == 0 {
		return RouteCredential{}, gerror.New("channel has no available upstream credential")
	}
	var binding entity.ApiKeyChannelCredentials
	if excluded == nil {
		if err = dao.ApiKeyChannelCredentials.Ctx(ctx).Where(do.ApiKeyChannelCredentials{ApiKeyId: apiKeyID, ChannelId: channelID}).Scan(&binding); err != nil {
			return RouteCredential{}, gerror.Wrap(err, "load channel credential binding")
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

func (s *Service) CredentialForTest(ctx context.Context, channelID, credentialID uint64) (RouteCredential, error) {
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

func (s *Service) clearCredentialTransient(ctx context.Context, credentialID uint64) {
	_ = s.app.Redis.Del(ctx, CredentialFailureKey(credentialID), CredentialCooldownKey(credentialID)).Err()
}

func CredentialFailureKey(credentialID uint64) string {
	return fmt.Sprintf("aiferry:credential:%d:failures", credentialID)
}

func CredentialCooldownKey(credentialID uint64) string {
	return fmt.Sprintf("aiferry:credential:%d:cooldown", credentialID)
}

func (s *Service) availableCredentials(ctx context.Context, channelID uint64, excluded map[uint64]struct{}) ([]credentialRow, error) {
	rows := make([]credentialRow, 0)
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{ChannelId: channelID, Status: 1}).OrderAsc(dao.ChannelCredentials.Columns().Id).Scan(&rows); err != nil {
		return nil, gerror.Wrap(err, "list available channel credentials")
	}
	available := make([]credentialRow, 0, len(rows))
	for _, row := range rows {
		if _, skip := excluded[row.Id]; skip {
			continue
		}
		if cooling, _ := s.app.Redis.Exists(ctx, CredentialCooldownKey(row.Id)).Result(); cooling > 0 {
			continue
		}
		available = append(available, row)
	}
	return available, nil
}

func (s *Service) credentialByID(ctx context.Context, channelID, credentialID uint64) (credentialRow, error) {
	var row credentialRow
	if err := dao.ChannelCredentials.Ctx(ctx).Where(do.ChannelCredentials{Id: credentialID, ChannelId: channelID}).Scan(&row); err != nil {
		return row, gerror.Wrap(err, "find channel credential")
	}
	if row.Id == 0 {
		return row, gerror.New("channel credential not found")
	}
	return row, nil
}

func (s *Service) bindFirstCredential(ctx context.Context, apiKeyID, channelID uint64, selected credentialRow, available []credentialRow) (credentialRow, error) {
	result, err := dao.ApiKeyChannelCredentials.Ctx(ctx).Data(do.ApiKeyChannelCredentials{
		ApiKeyId: apiKeyID, ChannelId: channelID, ChannelCredentialId: selected.Id,
	}).InsertIgnore()
	if err != nil {
		return credentialRow{}, gerror.Wrap(err, "create channel credential binding")
	}
	if inserted, _ := result.RowsAffected(); inserted > 0 {
		return selected, nil
	}

	var binding entity.ApiKeyChannelCredentials
	if err = dao.ApiKeyChannelCredentials.Ctx(ctx).Where(do.ApiKeyChannelCredentials{ApiKeyId: apiKeyID, ChannelId: channelID}).Scan(&binding); err != nil {
		return credentialRow{}, gerror.Wrap(err, "load concurrent channel credential binding")
	}
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

func (s *Service) replaceCredentialBinding(ctx context.Context, apiKeyID, channelID, credentialID uint64) error {
	if _, err := dao.ApiKeyChannelCredentials.Ctx(ctx).
		Where(do.ApiKeyChannelCredentials{ApiKeyId: apiKeyID, ChannelId: channelID}).
		Data(do.ApiKeyChannelCredentials{ChannelCredentialId: credentialID}).
		Update(); err != nil {
		return gerror.Wrap(err, "update channel credential binding")
	}
	return nil
}

func (s *Service) ensureCredentialMetadata(ctx context.Context, row *credentialRow) error {
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

func (s *Service) newCredentialData(value string) (do.ChannelCredentials, error) {
	cipherText, err := s.app.Secrets.Encrypt(strings.TrimSpace(value))
	if err != nil {
		return do.ChannelCredentials{}, err
	}
	return do.ChannelCredentials{
		KeyPrefix: maskedCredentialPrefix(value), KeyHash: upstreamKeyHash(value), ApiKeyCipher: cipherText, Status: 1,
	}, nil
}

func credentialView(row credentialRow) CredentialView {
	view := CredentialView{
		Id: row.Id, KeyPrefix: row.KeyPrefix, Status: row.Status, AutoDisabledReason: row.AutoDisabledReason,
		AutoDisabledStatusCode: row.AutoDisabledStatusCode, LastCostUsed: row.LastCostUsed, LastCostRemaining: row.LastCostRemaining,
		LastCostCurrency: row.LastCostCurrency, LastCostAt: row.LastCostAt, CreatedAt: row.CreatedAt,
	}
	if row.AutoDisabledAt != nil {
		view.AutoDisabled = true
		view.AutoDisabledAt = row.AutoDisabledAt
	}
	return view
}

func maskedCredentialPrefix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return "已配置"
	}
	return value[:8] + "..."
}

func upstreamKeyHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}
