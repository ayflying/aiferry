package channel

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
)

// Replace only this account's catalog, including on a successful empty result.
// Channel-level model metadata is a projection for display, never a routing grant.
func saveGeminiCredentialCatalog(ctx context.Context, channelID, credentialID uint64, values map[string]modelmetadata.Metadata) error {
	return dao.Channels.Transaction(ctx, func(txCtx context.Context, tx gdb.TX) error {
		var ch entity.Channels
		query := dao.Channels.Ctx(txCtx).Where(do.Channels{Id: channelID, Type: GeminiSubscriptionChannelType})
		if dao.Channels.DB().GetConfig().Type != "sqlite" {
			query = query.LockUpdate()
		}
		// Serializes concurrent syncs on the master, including regeneration of the projection.
		if err := query.Scan(&ch); err != nil || ch.Id == 0 {
			return gerror.New("Gemini 订阅渠道已变更，请重新同步")
		}
		var row credentialRow
		query = dao.ChannelCredentials.Ctx(txCtx).Where(do.ChannelCredentials{Id: credentialID, ChannelId: channelID, Status: 1})
		if dao.Channels.DB().GetConfig().Type != "sqlite" {
			query = query.LockUpdate()
		}
		if err := query.Scan(&row); err != nil || row.Id == 0 || row.AutoDisabledAt != nil || !strings.HasPrefix(row.ApiKeyCipher, geminiCredentialPrefix) {
			return gerror.New("Gemini 登录凭据已变更，请重新同步")
		}
		if _, err := tx.Exec("DELETE FROM gemini_credential_models WHERE channel_id = ? AND channel_credential_id = ?", channelID, credentialID); err != nil {
			return gerror.Wrap(err, "replace Gemini credential catalog")
		}
		for name, m := range values {
			if err := modelmetadata.Validate(m); err != nil {
				return err
			}
			encoded, err := json.Marshal(m)
			if err != nil {
				return gerror.Wrap(err, "encode Gemini credential model metadata")
			}
			if _, err = tx.Exec("INSERT INTO gemini_credential_models (channel_id, channel_credential_id, upstream_name, metadata) VALUES (?, ?, ?, ?)", channelID, credentialID, name, string(encoded)); err != nil {
				return gerror.Wrap(err, "insert Gemini credential catalog entry")
			}
		}
		return projectGeminiChannelMetadata(tx, channelID)
	})
}

func projectGeminiChannelMetadata(tx gdb.TX, channelID uint64) error {
	rows, err := tx.GetAll(`SELECT m.upstream_name, m.metadata
		FROM gemini_credential_models m JOIN channel_credentials c ON c.id = m.channel_credential_id AND c.channel_id = m.channel_id
		WHERE m.channel_id = ? AND c.deleted_at IS NULL ORDER BY m.channel_credential_id`, channelID)
	if err != nil {
		return gerror.Wrap(err, "load Gemini credential model metadata")
	}
	byName := make(map[string][]modelmetadata.Metadata)
	for _, row := range rows {
		var m modelmetadata.Metadata
		if err := json.Unmarshal(row["metadata"].Bytes(), &m); err != nil {
			return gerror.Wrap(err, "decode Gemini credential model metadata")
		}
		name := row["upstream_name"].String()
		byName[name] = append(byName[name], m)
	}
	if _, err = tx.Exec("DELETE FROM model_metadata WHERE channel_id = ?", channelID); err != nil {
		return gerror.Wrap(err, "replace Gemini channel model metadata")
	}
	for name, values := range byName {
		encoded, err := json.Marshal(modelmetadata.Aggregate(values))
		if err != nil {
			return gerror.Wrap(err, "encode Gemini channel model metadata")
		}
		if _, err = tx.Exec("INSERT INTO model_metadata (channel_id, model_name, metadata) VALUES (?, ?, ?)", channelID, name, string(encoded)); err != nil {
			return gerror.Wrap(err, "insert Gemini channel model metadata")
		}
	}
	return nil
}

func geminiCredentialIDsForUpstream(ctx context.Context, channelID uint64, upstreamName string) (map[uint64]struct{}, error) {
	rows, err := g.DB().GetAll(ctx, `SELECT m.channel_credential_id
		FROM gemini_credential_models m JOIN channel_credentials c ON c.id = m.channel_credential_id AND c.channel_id = m.channel_id
		WHERE m.channel_id = ? AND m.upstream_name = ? AND c.deleted_at IS NULL AND c.status = 1 AND c.auto_disabled_at IS NULL`, channelID, upstreamName)
	if err != nil {
		return nil, gerror.Wrap(err, "load Gemini credential model eligibility")
	}
	eligible := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		eligible[row["channel_credential_id"].Uint64()] = struct{}{}
	}
	return eligible, nil
}

type credentialRouteChannelTypeKey struct{}

// WithCredentialRouteChannelType accepts the type of an internally resolved
// route candidate, never request input. Gemini channels cannot change type.
func WithCredentialRouteChannelType(ctx context.Context, channelType string) context.Context {
	return context.WithValue(ctx, credentialRouteChannelTypeKey{}, channelType)
}

func routeCredentialEligibility(ctx context.Context, channelID, modelID uint64) (map[uint64]struct{}, error) {
	if channelType, _ := ctx.Value(credentialRouteChannelTypeKey{}).(string); channelType != "" {
		if channelType != GeminiSubscriptionChannelType {
			return nil, nil
		}
		return geminiRouteCredentialIDs(ctx, channelID, modelID)
	}
	// Query the persisted channel type even when modelID is absent: a missing
	// model or failed lookup must never make Gemini selection unrestricted.
	kind, err := g.DB().GetValue(ctx, "SELECT type FROM channels WHERE id = ? AND deleted_at IS NULL", channelID)
	if err != nil {
		return nil, gerror.Wrap(err, "load credential channel type")
	}
	if kind.IsNil() {
		return nil, gerror.New("渠道不存在")
	}
	if kind.String() != GeminiSubscriptionChannelType {
		return nil, nil
	}
	return geminiRouteCredentialIDs(ctx, channelID, modelID)
}

func geminiRouteCredentialIDs(ctx context.Context, channelID, modelID uint64) (map[uint64]struct{}, error) {
	if modelID == 0 {
		return nil, gerror.New("Gemini 订阅路由需要有效的渠道模型")
	}
	// SQLite uses binary collation for the catalog and MySQL explicitly assigns
	// utf8mb4_bin. Compare the IDs in Go too to avoid mixed collations in JOIN.
	rows, err := g.DB().GetAll(ctx, `SELECT m.channel_credential_id, m.upstream_name, r.upstream_name AS routed_name
		FROM channel_models r
		JOIN gemini_credential_models m ON m.channel_id = r.channel_id AND m.upstream_name = r.upstream_name COLLATE `+geminiModelNameCollation()+`
		JOIN channel_credentials c ON c.id = m.channel_credential_id AND c.channel_id = m.channel_id
		WHERE r.channel_id = ? AND r.id = ? AND r.deleted_at IS NULL AND c.deleted_at IS NULL AND c.status = 1 AND c.auto_disabled_at IS NULL`, channelID, modelID)
	if err != nil {
		return nil, gerror.Wrap(err, "load Gemini route credential model eligibility")
	}
	eligible := make(map[uint64]struct{}, len(rows))
	for _, row := range rows {
		if row["upstream_name"].String() == row["routed_name"].String() {
			eligible[row["channel_credential_id"].Uint64()] = struct{}{}
		}
	}
	return eligible, nil
}

func geminiModelNameCollation() string {
	if g.DB().GetConfig().Type == "sqlite" {
		return "BINARY"
	}
	return "utf8mb4_bin"
}

func filterEligibleCredentials(rows []credentialRow, eligible map[uint64]struct{}) []credentialRow {
	if eligible == nil { // Standard channel types have no account model partition.
		return rows
	}
	result := make([]credentialRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := eligible[row.Id]; ok {
			result = append(result, row)
		}
	}
	return result
}
