package channel

import (
	"context"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
	"github.com/yunloli/aiferry/internal/dao"
	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
	"github.com/yunloli/aiferry/internal/model/do"
	"github.com/yunloli/aiferry/internal/model/entity"
	"net/http"
	"strings"
)

func (s *sChannel) DiscoverModels(ctx context.Context, channelID uint64) ([]DiscoveredModel, error) {
	channel, err := s.GetOwned(ctx, channelID)
	if err != nil {
		return nil, err
	}
	_, config, err := s.types.GetByCode(ctx, channel.Type)
	if err != nil {
		return nil, err
	}
	endpoint, err := resolveEndpointURL(channel.BaseUrl, config.Models.Path)
	if err != nil {
		return nil, err
	}
	credential, err := s.CredentialForTest(ctx, channelID, 0)
	if err != nil && config.Models.AuthType != "management_key" && config.Models.AuthType != "none" {
		return nil, err
	}
	body, err := s.fetchUpstreamJSON(ctx, channel, credential.APIKeyCipher, upstreamJSONRequest{
		Method:       config.Models.Method,
		Endpoint:     endpoint,
		AuthType:     config.Models.AuthType,
		HeaderName:   config.Models.HeaderName,
		HeaderPrefix: config.Models.HeaderPrefix,
		BodyLimit:    4 << 20,
		RequestError: "create model discovery request",
		FetchError:   "fetch upstream models",
		ReadError:    "read upstream models",
		InvalidError: "upstream model query returned invalid JSON",
		StatusError:  upstreamModelQueryError,
	})
	if err != nil {
		return nil, err
	}
	var existing []entity.ChannelModels
	if err = dao.ChannelModels.Ctx(ctx).
		Where(do.ChannelModels{ChannelId: channelID}).
		Scan(&existing); err != nil {
		return nil, gerror.Wrap(err, "load channel models")
	}
	existingByUpstream := make(map[string]entity.ChannelModels, len(existing))
	for _, model := range existing {
		if current, exists := existingByUpstream[model.UpstreamName]; !exists || (current.Enabled == 0 && model.Enabled == 1) {
			existingByUpstream[model.UpstreamName] = model
		}
	}

	names, err := modelNamesFromJSON(body, config.Models.ListPath, config.Models.IDPath)
	if err != nil {
		return nil, err
	}
	metadata, err := modelMetadataFromJSON(body, config.Models.ListPath, config.Models.IDPath)
	if err != nil {
		return nil, err
	}
	if err = modelmetadata.SaveUpstream(ctx, channelID, metadata); err != nil {
		return nil, err
	}
	s.InvalidateListCache(ctx)
	if err = s.invalidateRoutes(ctx); err != nil {
		return nil, err
	}
	models := make([]DiscoveredModel, 0, len(names))
	for _, name := range names {
		model, exists := existingByUpstream[name]
		models = append(models, DiscoveredModel{
			Metadata:   metadata[name],
			Name:       name,
			PublicName: stringOrDefault(model.PublicName, name),
			Selected:   exists && model.Enabled == 1,
		})
	}
	return models, nil
}

func modelNamesFromJSON(body []byte, listPath, idPath string) ([]string, error) {
	items := gjson.ParseBytes(body)
	if listPath != "" {
		items = gjson.GetBytes(body, listPath)
	}
	if !items.IsArray() {
		return nil, gerror.New("model list path did not resolve to an array")
	}
	names := make([]string, 0, len(items.Array()))
	for _, item := range items.Array() {
		name := strings.TrimSpace(item.Get(idPath).String())
		if name != "" {
			names = append(names, name)
		}
	}
	return normalizeModelNames(names), nil
}

func upstreamModelQueryError(status int, body []byte) error {
	if status != http.StatusTooManyRequests {
		return gerror.Newf("upstream model query returned HTTP %d", status)
	}

	var (
		code    = strings.ToUpper(strings.TrimSpace(firstJSONText(body, "code", "error.code")))
		message = strings.ToUpper(strings.TrimSpace(firstJSONText(body, "message", "error.message")))
	)
	if strings.Contains(code, "DAILY_LIMIT_EXCEEDED") ||
		(strings.Contains(code, "USAGE_LIMIT_EXCEEDED") &&
			(strings.Contains(message, "DAILY_LIMIT_EXCEEDED") || strings.Contains(message, "DAILY USAGE LIMIT"))) {
		return gerror.New("上游每日用量额度已用尽，请在上游补充额度或等待每日额度重置")
	}
	return gerror.New("上游请求受限（HTTP 429），请稍后重试或检查上游额度")
}

func firstJSONText(body []byte, paths ...string) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}
