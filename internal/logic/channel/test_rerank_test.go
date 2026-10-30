package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
)

func TestRerankAutoSelection(t *testing.T) {
	config := channeltype.DefaultConfig()
	config.Protocol.ChatCompletionsOnly = true
	config.Protocol.RerankModels = []string{"custom-ranker", "vendor/rank-*"}
	config.Protocol.MessagesModels = []string{"*"}
	for _, model := range []string{"BAAI/bge-reranker-v2-m3", "rerank-v3.5", " Qwen3-Reranker-8B ", "custom-ranker", "vendor/rank-v1"} {
		endpoints := testEndpoints("auto", model, config, adminapi.SystemResilienceSettingsInput{})
		if len(endpoints) != 1 || endpoints[0] != "rerank" {
			t.Errorf("rerank should not fall back to chat for %q: %v", model, endpoints)
		}
	}
	if got := testEndpoints("embeddings", "rerank-v3.5", config, adminapi.SystemResilienceSettingsInput{}); len(got) != 1 || got[0] != "embeddings" {
		t.Fatalf("explicit selection must win: %v", got)
	}
}

func TestRerankRequestRemainsStandardNonStreamingJSON(t *testing.T) {
	config := channeltype.DefaultConfig()
	config.Protocol.ForceUpstreamStream = true
	config.Endpoints["rerank"] = channeltype.EndpointConfig{Path: "/v2/rerank"}
	path, payload, streamed := testPayload("rerank", "bge-reranker-v2-m3", true, config, adminapi.SystemResilienceSettingsInput{})
	if path != "/v2/rerank" || streamed {
		t.Fatalf("unexpected rerank request metadata: %q %v", path, streamed)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != path || req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected HTTP request: %s %s %v", req.Method, req.URL.Path, req.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body) != 5 || body["model"] != "bge-reranker-v2-m3" || body["query"] == "" || body["top_n"] != float64(1) || body["return_documents"] != true {
			t.Errorf("unexpected standard rerank body: %#v", body)
		}
		if docs, ok := body["documents"].([]any); !ok || len(docs) != 2 {
			t.Errorf("missing rerank documents: %#v", body)
		}
		_, _ = io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.99}],"usage":{"total_tokens":12}}`)
	}))
	defer server.Close()
	// Even OpenCode chat-only adaptations must not inject stream/tools/cache fields.
	req, forcedStream, err := buildTestRequest(context.Background(), resolveTestURL(server.URL, path), OpenCodeZenChannelType, payload, AdvancedConfig{}, "rerank-check")
	if err != nil || forcedStream {
		t.Fatalf("build rerank request: forcedStream=%v err=%v", forcedStream, err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if tokens := parseTestUsage(body, false); tokens.Total == nil || *tokens.Total != 12 {
		t.Fatalf("rerank JSON usage should use existing parser: %+v", tokens)
	}
}
