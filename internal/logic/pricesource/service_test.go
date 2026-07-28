package pricesource

import "testing"

func TestParseConfigNormalizesBaseLLMSource(t *testing.T) {
	config, err := ParseConfig([]byte(`{
    "baseUrl":"https://basellm.github.io/",
    "pricing":{"adapter":"newapi_ratio","path":"/llm-metadata/api/newapi/ratio_config-v1-base.json","authType":"none"}
  }`))
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://basellm.github.io" || config.Pricing.Method != "GET" {
		t.Fatalf("unexpected source config: %+v", config)
	}
}

func TestParseConfigRejectsProtectedSource(t *testing.T) {
	_, err := ParseConfig([]byte(`{
    "baseUrl":"https://prices.example.com",
    "pricing":{"adapter":"json","path":"/v1/prices","authType":"channel_key","modelPath":"model","ratesPath":"rates"}
  }`))
	if err == nil {
		t.Fatal("expected protected source rejection")
	}
}
