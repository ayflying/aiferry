package relay

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/tidwall/gjson"

	"github.com/yunloli/aiferry/internal/logic/usage"
)

// validateRerankRequest 只校验各家重排接口共有的结构。文档对象字段由上游
// 决定（如 text/title），网关不改写文档，保留索引与相关性评分的对应关系。
func validateRerankRequest(body []byte) error {
	query := gjson.GetBytes(body, "query")
	if query.Type != gjson.String || strings.TrimSpace(query.String()) == "" {
		return gerror.New("query must be a non-empty string")
	}
	documents := gjson.GetBytes(body, "documents")
	if !documents.IsArray() || len(documents.Array()) == 0 {
		return gerror.New("documents must be a non-empty array")
	}
	for _, document := range documents.Array() {
		if document.Type != gjson.String && !document.IsObject() {
			return gerror.New("documents must contain strings or objects")
		}
	}
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() && stream.Type != gjson.False {
		return gerror.New("rerank does not support streaming; omit stream or set it to false")
	}
	if topN := gjson.GetBytes(body, "top_n"); topN.Exists() && (topN.Type != gjson.Number || topN.Float() < 1 || topN.Float() != float64(topN.Int())) {
		return gerror.New("top_n must be a positive integer")
	}
	return nil
}

// rerankUpstreamURL 与管理端模型测试读取同一份端点配置，兼容 Cohere
// /v2/rerank 等地址；计费仍按客户端 /rerank 端点归类，不按实际 URL 分裂规则。
func (s *sRelay) rerankUpstreamURL(ctx context.Context, candidate Candidate) string {
	if config, ok := s.protocolTypeConfig(ctx, candidate); ok {
		if endpoint, exists := config.Endpoints["rerank"]; exists && strings.TrimSpace(endpoint.Path) != "" {
			return resolveUpstreamURL(strings.TrimRight(candidate.BaseURL, "/"), endpoint.Path)
		}
	}
	return resolveUpstreamURL(strings.TrimRight(candidate.BaseURL, "/"), "/rerank")
}

func parseResponseUsage(endpoint string, body []byte) usage.TokenUsage {
	if endpoint == "/rerank" {
		return usage.ParseRerankUsage(body)
	}
	return parseJSONUsage(body)
}
