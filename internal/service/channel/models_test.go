package channel

import (
	"reflect"
	"testing"
)

func TestNormalizeModelNamesSortsAndDeduplicates(t *testing.T) {
	input := []string{" zeta ", "Alpha", "beta", "Alpha", "", "alpha"}
	want := []string{"Alpha", "alpha", "beta", "zeta"}

	if got := normalizeModelNames(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected model names: got %v, want %v", got, want)
	}
}

func TestModelNamesFromCustomJSONPaths(t *testing.T) {
	names, err := modelNamesFromJSON([]byte(`{"payload":{"models":[{"name":"zeta"},{"name":"alpha"},{"name":"alpha"}]}}`), "payload.models", "name")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("unexpected names: got %v, want %v", names, want)
	}
}

func TestModelNamesRejectsNonArrayPath(t *testing.T) {
	if _, err := modelNamesFromJSON([]byte(`{"payload":{"model":"gpt"}}`), "payload.model", "id"); err == nil {
		t.Fatal("expected non-array model path to fail")
	}
}

func TestUpstreamModelQueryErrorExplainsDailyUsageLimit(t *testing.T) {
	err := upstreamModelQueryError(429, []byte(`{
  "code": "USAGE_LIMIT_EXCEEDED",
  "message": "error: code=429 reason=\"DAILY_LIMIT_EXCEEDED\" message=\"daily usage limit exceeded\""
}`))
	if err == nil || err.Error() != "上游每日用量额度已用尽，请在上游补充额度或等待每日额度重置" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpstreamModelQueryErrorKeepsUnknownFailuresGeneric(t *testing.T) {
	if err := upstreamModelQueryError(429, []byte(`{"code":"RATE_LIMIT"}`)); err == nil || err.Error() != "上游请求受限（HTTP 429），请稍后重试或检查上游额度" {
		t.Fatalf("unexpected 429 error: %v", err)
	}
	if err := upstreamModelQueryError(401, nil); err == nil || err.Error() != "upstream model query returned HTTP 401" {
		t.Fatalf("unexpected non-429 error: %v", err)
	}
}
