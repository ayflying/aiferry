package usage

import "testing"

func TestParseRerankUsageJinaTotalOnlyMapsToInput(t *testing.T) {
	got := ParseRerankUsage([]byte(`{"usage":{"total_tokens":123}}`))
	if got.Input == nil || *got.Input != 123 {
		t.Fatalf("input = %v, want 123", got.Input)
	}
	if got.Output == nil || *got.Output != 0 {
		t.Fatalf("output = %v, want explicit zero", got.Output)
	}
	if got.Total == nil || *got.Total != 123 {
		t.Fatalf("total = %v, want 123", got.Total)
	}
}

func TestParseRerankUsageCohereUsesMetaTokens(t *testing.T) {
	got := ParseRerankUsage([]byte(`{"meta":{"tokens":{"input_tokens":11,"output_tokens":7},"billed_units":{"search_units":999}}}`))
	if got.Input == nil || *got.Input != 11 || got.Output == nil || *got.Output != 7 {
		t.Fatalf("tokens = %+v, want input=11 output=7", got)
	}
	if got.Total == nil || *got.Total != 18 {
		t.Fatalf("total = %v, want 18", got.Total)
	}
}

func TestParseRerankUsageDoesNotInventTokens(t *testing.T) {
	got := ParseRerankUsage([]byte(`{"meta":{"billed_units":{"search_units":3}}}`))
	if got.Input != nil || got.Output != nil || got.Total != nil {
		t.Fatalf("usage = %+v, token fields must remain absent", got)
	}
}

func TestParseRerankUsagePrefersExistingTokenUsage(t *testing.T) {
	got := ParseRerankUsage([]byte(`{"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6},"meta":{"tokens":{"input_tokens":99,"output_tokens":99}}}`))
	if got.Input == nil || *got.Input != 4 || got.Output == nil || *got.Output != 2 || got.Total == nil || *got.Total != 6 {
		t.Fatalf("usage = %+v, standard token fields should win", got)
	}
	got = ParseRerankUsage([]byte(`{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":99}}`))
	if got.Input == nil || *got.Input != 0 || got.Output == nil || *got.Output != 0 {
		t.Fatalf("explicit zero token counts must not be replaced by total: %+v", got)
	}
}

func TestParseRerankUsageDoesNotChangeChatUsage(t *testing.T) {
	got := ParseJSONUsage([]byte(`{"usage":{"total_tokens":123},"meta":{"tokens":{"input_tokens":99,"output_tokens":1}}}`))
	if got.Input != nil || got.Output != nil || got.Total == nil || *got.Total != 123 {
		t.Fatalf("chat total-only usage must remain unchanged: %+v", got)
	}
}

func TestParseRerankUsageMissingTokensStayAbsent(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"results":[{"index":0,"relevance_score":0.9}]}`,
		`{"meta":{"tokens":{},"billed_units":{"input_tokens":100,"search_units":1}}}`,
		`{"usage":{"total_tokens":"123"}}`,
	} {
		got := ParseRerankUsage([]byte(body))
		if got.Input != nil || got.Output != nil || got.Total != nil {
			t.Fatalf("body %s: must not invent token usage: %+v", body, got)
		}
	}
}
