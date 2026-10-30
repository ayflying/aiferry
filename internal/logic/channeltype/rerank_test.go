package channeltype

import "testing"

func TestRerankEndpointDefaultsAndValidation(t *testing.T) {
	config, err := ParseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := config.Endpoints["rerank"]
	if endpoint.Method != "POST" || endpoint.Path != "/rerank" || endpoint.RequestBody != "json" || endpoint.SupportsStream || endpoint.AuthType != AuthChannelKey {
		t.Fatalf("unexpected rerank endpoint: %+v", endpoint)
	}
	for _, raw := range []string{
		`{"endpoints":{"rerank":{"method":"GET","path":"/rerank","requestBody":"json"}}}`,
		`{"endpoints":{"rerank":{"method":"POST","path":"/rerank","requestBody":"multipart"}}}`,
		`{"endpoints":{"rerank":{"method":"POST","path":"/rerank","requestBody":"json","supportsStream":true}}}`,
	} {
		if _, err := ParseConfig([]byte(raw)); err == nil {
			t.Fatalf("invalid rerank configuration accepted: %s", raw)
		}
	}
	config, err = ParseConfig([]byte(`{"protocol":{"rerankModels":["custom-ranker","vendor/rank-*"]},"endpoints":{"rerank":{"method":"post","path":" /v2/rank ","requestBody":"JSON","authType":"channel_key","headerName":"X-Key"}}}`))
	if err != nil || config.Endpoints["rerank"].Path != "/v2/rank" || config.Endpoints["rerank"].HeaderName != "X-Key" {
		t.Fatalf("unexpected configured rerank endpoint: %+v err=%v", config, err)
	}
	for _, model := range []string{"custom-ranker", "VENDOR/RANK-v1", "BAAI/bge-reranker-v2-m3", " rerank-v3.5 "} {
		if !MatchesRerankModel(config, model) {
			t.Errorf("rerank model not recognized: %q", model)
		}
	}
	for _, model := range []string{"", "gpt-4.1-mini", "text-embedding-3-small", "vendor/rankx"} {
		if MatchesRerankModel(config, model) {
			t.Errorf("ordinary model recognized as rerank: %q", model)
		}
	}
}
