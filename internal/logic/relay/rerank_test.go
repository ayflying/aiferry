package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

func TestValidateRerankRequest(t *testing.T) {
	tests := []struct {
		body  string
		valid bool
	}{
		{`{"query":"问题","documents":["文档",{"title":"标题","text":"正文"}],"top_n":1}`, true},
		{`{"query":"问题","documents":["正文"],"stream":false}`, true},
		{`{"documents":["正文"]}`, false},
		{`{"query":" ","documents":["正文"]}`, false},
		{`{"query":"问题","documents":[]}`, false},
		{`{"query":"问题","documents":[123]}`, false},
		{`{"query":"问题","documents":["正文"],"stream":true}`, false},
		{`{"query":"问题","documents":["正文"],"stream":"false"}`, false},
		{`{"query":"问题","documents":["正文"],"top_n":0}`, false},
		{`{"query":"问题","documents":["正文"],"top_n":1.5}`, false},
	}
	for _, test := range tests {
		if err := validateRerankRequest([]byte(test.body)); (err == nil) != test.valid {
			t.Errorf("请求 %s 校验结果 %v，期望合法=%t", test.body, err, test.valid)
		}
	}
}

func TestRerankAttemptPreservesProtocol(t *testing.T) {
	response := `{"id":"rank-1","results":[{"index":1,"relevance_score":0.98,"document":{"text":"武汉"}}],"usage":{"total_tokens":12}}`
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Rerank-Key") != "test-key" || r.Header.Get("Authorization") != "" {
			t.Errorf("重排独立鉴权未生效：%v", r.Header)
		}
		if r.URL.Path != "/custom/rerank" || r.Method != http.MethodPost {
			t.Errorf("错误的上游请求 %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["model"] != "claude-rerank" || body["query"] != "湖北省会" || body["top_n"] != float64(1) || body["return_documents"] != true {
			t.Errorf("请求字段丢失：%v", body)
		}
		if len(body["documents"].([]any)) != 2 {
			t.Errorf("文档丢失：%v", body)
		}
		for _, field := range []string{"stream", "prompt_cache_key", "messages", "unknown"} {
			if _, ok := body[field]; ok {
				t.Errorf("不应下发字段 %s", field)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer upstream.Close()
	secrets, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	appSvc := &app.Service{Secrets: secrets, HTTP: upstream.Client()}
	builtins := &config.BuiltinRegistry{ChannelTypes: []config.BuiltinChannelType{{
		ID: 99, Name: "重排测试", Code: "probe_rerank",
		Config: json.RawMessage(`{"models":{"path":"/models","idPath":"id","authType":"channel_key"},"costs":{"adapter":"none"},"endpoints":{"rerank":{"method":"POST","path":"/custom/rerank","requestBody":"json","authType":"channel_key","headerName":"X-Rerank-Key","headerPrefix":""}}}`),
	}}}
	types := channeltype.New(builtins, nil)
	relay := &sRelay{app: appSvc, types: types, channels: channel.New(appSvc, types, nil, nil, nil, nil, nil, nil), prices: pricingcache.New()}
	cipher, err := secrets.Encrypt("test-key")
	if err != nil {
		t.Fatal(err)
	}
	settings := adminapi.SystemResilienceSettingsInput{ProtocolConversionEnabled: true, NonStreamTimeoutSeconds: 30, MessagesModels: []string{"claude-*"}}
	candidate := Candidate{ChannelID: 7, ChannelName: "rerank-test", ChannelType: "probe_rerank", BaseURL: upstream.URL, APIKeyCipher: cipher, UpstreamName: "claude-rerank", AdvancedConfig: `{"forceOpenAIFormat":true}`}
	body := []byte(`{"model":"公开模型","query":"湖北省会","documents":["上海",{"text":"武汉"}],"top_n":1,"return_documents":true,"stream":false,"unknown":"移除"}`)
	result, handled, err := relay.attempt(context.Background(), nil, http.Header{}, "/rerank", body, candidate, false, 11, 42, settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handled || calls != 1 || result.status != 200 || result.upstreamEndpoint != "/rerank" || result.protocolConversion != "" {
		t.Fatalf("非直通结果：%+v, calls=%d", result, calls)
	}
	if string(result.body) != response {
		t.Fatalf("重排响应被改写：%s", result.body)
	}
	if result.tokens.Input == nil || *result.tokens.Input != 12 || result.tokens.Output == nil || *result.tokens.Output != 0 {
		t.Fatalf("用量未正确解析：%+v", result.tokens)
	}
}
