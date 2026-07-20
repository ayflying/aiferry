package channeltype

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yunloli/aiferry/internal/config"
)

func TestGetByCodeUsesBuiltinConfiguration(t *testing.T) {
	builtins := &config.BuiltinRegistry{ChannelTypes: []config.BuiltinChannelType{{
		ID: 42, Name: "OpenAI", Code: "openai", Config: json.RawMessage(`{"models":{"path":"/models","idPath":"id"},"costs":{"adapter":"none"}}`),
	}}}
	row, parsed, err := New(builtins).GetByCode(context.Background(), "openai")
	if err != nil {
		t.Fatal(err)
	}
	if row.Id != 42 || row.BuiltIn != 1 || parsed.Models.Path != "/models" {
		t.Fatalf("unexpected built-in channel type: %+v %+v", row, parsed)
	}
}

func TestParseConfigNormalizesDefaults(t *testing.T) {
	config, err := ParseConfig([]byte(`{"models":{"path":"/models","idPath":"id"},"costs":{"adapter":"none"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://api.openai.com/v1" || config.Models.Method != "GET" || config.Models.AuthType != AuthChannelKey || config.Costs.Adapter != AdapterNone {
		t.Fatalf("unexpected normalized config: %+v", config)
	}
}

func TestParseConfigRejectsInvalidDefaultBaseURL(t *testing.T) {
	_, err := ParseConfig([]byte(`{"baseUrl":"ftp://example.com","models":{"path":"/models","idPath":"id"},"costs":{"adapter":"none"}}`))
	if err == nil {
		t.Fatal("expected invalid base URL rejection")
	}
}

func TestParseConfigUsesOpenAIDefaultsForEmptyInput(t *testing.T) {
	config, err := ParseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.Models.Path != "/models" || config.Costs.Adapter != AdapterOpenAICosts {
		t.Fatalf("unexpected OpenAI defaults: %+v", config)
	}
	for _, endpoint := range []string{"chatCompletions", "responses", "imagesGenerations", "audioSpeech", "audioTranscriptions", "videoGenerations", "videoContent", "realtimeSessions"} {
		if _, ok := config.Endpoints[endpoint]; !ok {
			t.Fatalf("default OpenAI endpoint is missing: %s", endpoint)
		}
	}
}

func TestParseConfigRejectsInvalidEndpoint(t *testing.T) {
	_, err := ParseConfig([]byte(`{"endpoints":{"custom":{"method":"PATCH","path":"/custom","requestBody":"json","authType":"channel_key"}}}`))
	if err == nil {
		t.Fatal("expected unsupported endpoint method to fail validation")
	}
}

func TestParseConfigRejectsUnknownFields(t *testing.T) {
	_, err := ParseConfig([]byte(`{"models":{"path":"/models","idPath":"id","listPatch":"data"},"costs":{"adapter":"none"}}`))
	if err == nil {
		t.Fatal("expected unknown JSON field to fail validation")
	}
}

func TestParseConfigRoundTrip(t *testing.T) {
	raw := []byte(`{"models":{"method":"GET","path":"/models","listPath":"data","idPath":"id","authType":"channel_key","headerPrefix":"Bearer "},"costs":{"adapter":"custom_json","path":"/usage","authType":"channel_key","remainingPath":"balance","fixedCurrency":"usd"}}`)
	config, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(config)
	if !json.Valid(encoded) || config.Costs.FixedCurrency != "USD" || config.Models.HeaderName != "Authorization" {
		t.Fatalf("unexpected config: %s", encoded)
	}
}

func TestParseConfigAcceptsJSONPriceSynchronization(t *testing.T) {
	config, err := ParseConfig([]byte(`{
    "models":{"path":"/models","idPath":"id","authType":"channel_key"},
    "costs":{"adapter":"none"},
    "pricing":{"adapter":"json","path":"/pricing","authType":"channel_key","listPath":"data","modelPath":"model","ratesPath":"rates"}
  }`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Pricing.Adapter != "json" || config.Pricing.Method != "GET" {
		t.Fatalf("unexpected pricing config: %+v", config.Pricing)
	}
}

func TestParsePricingConfigAcceptsNewAPIRatioSource(t *testing.T) {
	config, err := ParsePricingConfig(PricingConfig{
		Adapter:  AdapterNewAPIRatio,
		Path:     "/llm-metadata/api/newapi/ratio_config-v1-base.json",
		AuthType: AuthNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.Adapter != AdapterNewAPIRatio || config.Method != "GET" {
		t.Fatalf("unexpected config: %+v", config)
	}
}

func TestParseConfigRejectsNewAPIRatioChannelType(t *testing.T) {
	_, err := ParseConfig([]byte(`{
    "models":{"path":"/models","idPath":"id"},
    "costs":{"adapter":"none"},
    "pricing":{"adapter":"newapi_ratio","path":"/ratio","authType":"none"}
  }`))
	if err == nil {
		t.Fatal("expected price source adapter rejection")
	}
}
