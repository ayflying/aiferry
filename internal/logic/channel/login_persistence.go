package channel

import (
	"context"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/model/do"
)

// insertLoginCredential never updates a duplicate account. The database's unique
// (channel_id, key_hash) constraint is authoritative even across concurrent sessions.
func insertLoginCredential(ctx context.Context, channelID uint64, hash, prefix, cipher string) (uint64, error) {
	if dao.ChannelCredentials.DB().GetConfig().Type != "sqlite" {
		return insertUniqueLoginCredential(ctx, channelID, hash, prefix, cipher)
	}
	// Legacy SQLite schemas do not have the MySQL unique constraint. The
	// read and insert share a transaction; competing writes cannot both commit.
	var id uint64
	err := dao.ChannelCredentials.Transaction(ctx, func(txCtx context.Context, _ gdb.TX) error {
		count, err := dao.ChannelCredentials.Ctx(txCtx).Where(do.ChannelCredentials{ChannelId: channelID, KeyHash: hash}).Count()
		if err != nil {
			return gerror.New("读取登录凭据失败，请重试")
		}
		if count != 0 {
			return gerror.New("该渠道已添加此账号，请对原凭据使用重新登录")
		}
		id, err = insertUniqueLoginCredential(txCtx, channelID, hash, prefix, cipher)
		return err
	})
	return id, err
}

func insertUniqueLoginCredential(ctx context.Context, channelID uint64, hash, prefix, cipher string) (uint64, error) {
	data := do.ChannelCredentials{ChannelId: channelID, KeyHash: hash, KeyPrefix: prefix, ApiKeyCipher: cipher, Status: 1}
	id, err := dao.ChannelCredentials.Ctx(ctx).Data(data).InsertAndGetId()
	if err != nil {
		var duplicate *mysql.MySQLError
		if (errors.As(err, &duplicate) && duplicate.Number == 1062) || strings.Contains(err.Error(), "UNIQUE constraint failed: channel_credentials.channel_id, channel_credentials.key_hash") {
			return 0, gerror.New("该渠道已添加此账号，请对原凭据使用重新登录")
		}
		return 0, credentialInsertError(err)
	}
	if id <= 0 {
		return 0, gerror.New("保存登录凭据失败，请重试")
	}
	return uint64(id), nil
}

// replaceLoginCredential updates only the token envelope. A refresh, deletion,
// or another successful login after the caller read row cannot be overwritten.
func replaceLoginCredential(ctx context.Context, row credentialRow, cipher string) error {
	where := do.ChannelCredentials{Id: row.Id, ChannelId: row.ChannelId, KeyHash: row.KeyHash, ApiKeyCipher: row.ApiKeyCipher}
	result, err := dao.ChannelCredentials.Ctx(ctx).Where(where).Data(do.ChannelCredentials{ApiKeyCipher: cipher}).Update()
	if err != nil {
		return gerror.New("更新登录凭据失败，请重试")
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return gerror.New("登录凭据已变更，请重新发起授权")
	}
	return nil
}

func (s *sChannel) invalidateLoginCredentials(ctx context.Context) {
	s.invalidateCredentialCache(ctx)
	s.InvalidateListCache(ctx)
	_ = s.invalidateRoutes(ctx)
}
