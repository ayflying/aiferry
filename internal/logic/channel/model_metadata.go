package channel

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"
	"github.com/yunloli/aiferry/internal/logic/modelmetadata"
)

func modelMetadataFromJSON(body []byte, listPath, idPath string) (map[string]modelmetadata.Metadata, error) {
	items := gjson.ParseBytes(body)
	if listPath != "" {
		items = items.Get(listPath)
	}
	if !items.IsArray() {
		return nil, gerror.New("model list path did not resolve to an array")
	}
	parsed := map[string][]modelmetadata.Metadata{}
	for _, item := range items.Array() {
		name := strings.TrimSpace(item.Get(idPath).String())
		if name == "" {
			continue
		}
		parsed[name] = append(parsed[name], modelmetadata.Parse(item))
	}
	result := map[string]modelmetadata.Metadata{}
	for name, values := range parsed {
		result[name] = modelmetadata.Aggregate(values)
	}
	return result, nil
}

func (s *sChannel) GetModelMetadata(ctx context.Context, publicName string) (modelmetadata.View, error) {
	return modelmetadata.Get(ctx, strings.TrimSpace(publicName))
}
func (s *sChannel) PutModelMetadata(ctx context.Context, publicName string, metadata *modelmetadata.Metadata) (modelmetadata.View, error) {
	publicName = strings.TrimSpace(publicName)
	if err := modelmetadata.PutManual(ctx, publicName, metadata); err != nil {
		return modelmetadata.View{}, err
	}
	s.InvalidateListCache(ctx)
	if err := s.invalidateRoutes(ctx); err != nil {
		return modelmetadata.View{}, err
	}
	return modelmetadata.Get(ctx, publicName)
}
