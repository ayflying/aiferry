package channeltype

import (
	"encoding/json"
	"testing"
)

func TestParseConfigNormalizesDefaults(t *testing.T) {
	config, err := ParseConfig([]byte(`{"models":{"path":"/models","idPath":"id"},"costs":{"adapter":"none"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Models.Method != "GET" || config.Models.AuthType != "none" || config.Costs.Adapter != "none" {
		t.Fatalf("unexpected normalized config: %+v", config)
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

func TestParseConfigAcceptsPriceSyncOnlyNewAPIRatioSource(t *testing.T) {
	config, err := ParseConfig([]byte(`{
    "priceSyncOnly":true,
    "costs":{"adapter":"none"},
    "pricing":{"adapter":"newapi_ratio","path":"/llm-metadata/api/newapi/ratio_config-v1-base.json","authType":"none"}
  }`))
	if err != nil {
		t.Fatal(err)
	}
	if !config.PriceSyncOnly || config.Pricing.Adapter != AdapterNewAPIRatio {
		t.Fatalf("unexpected config: %+v", config)
	}
}
