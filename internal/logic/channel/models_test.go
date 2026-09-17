package channel

import (
	"reflect"
	"testing"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func TestNormalizeModelNamesSortsAndDeduplicates(t *testing.T) {
	input := []string{" zeta ", "Alpha", "beta", "Alpha", "", "alpha"}
	want := []string{"Alpha", "alpha", "beta", "zeta"}

	if got := normalizeModelNames(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected model names: got %v, want %v", got, want)
	}
}

func TestNormalizeModelMappingsSupportsAliasesAndLegacyNames(t *testing.T) {
	mappings, err := normalizeModelMappings(adminapi.ModelSelectionInput{Models: []adminapi.ModelMappingInput{
		{UpstreamName: " gpt-5.6 ", PublicName: " gpt-5.6-luna "},
		{UpstreamName: "gpt-4.1", PublicName: ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []modelMapping{
		{UpstreamName: "gpt-5.6", PublicName: "gpt-5.6-luna"},
		{UpstreamName: "gpt-4.1", PublicName: "gpt-4.1"},
	}
	if !reflect.DeepEqual(mappings, want) {
		t.Fatalf("unexpected mappings: got %#v, want %#v", mappings, want)
	}

	legacy, err := normalizeModelMappings(adminapi.ModelSelectionInput{ModelNames: []string{"zeta", "alpha", "zeta"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []modelMapping{{UpstreamName: "alpha", PublicName: "alpha"}, {UpstreamName: "zeta", PublicName: "zeta"}}; !reflect.DeepEqual(legacy, want) {
		t.Fatalf("unexpected legacy mappings: got %#v, want %#v", legacy, want)
	}
}

func TestNormalizeModelMappingsAllowsMultipleAliasesForUpstreamModel(t *testing.T) {
	mappings, err := normalizeModelMappings(adminapi.ModelSelectionInput{Models: []adminapi.ModelMappingInput{
		{UpstreamName: "gpt-5", PublicName: "gpt-5-main"},
		{UpstreamName: "gpt-5", PublicName: "gpt-5-backup"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []modelMapping{
		{UpstreamName: "gpt-5", PublicName: "gpt-5-main"},
		{UpstreamName: "gpt-5", PublicName: "gpt-5-backup"},
	}
	if !reflect.DeepEqual(mappings, want) {
		t.Fatalf("unexpected mappings: got %#v, want %#v", mappings, want)
	}
}

func TestNormalizeModelMappingsRejectsDuplicateMapping(t *testing.T) {
	_, err := normalizeModelMappings(adminapi.ModelSelectionInput{Models: []adminapi.ModelMappingInput{
		{UpstreamName: "gpt-5", PublicName: "gpt-5-main"},
		{UpstreamName: "gpt-5", PublicName: "gpt-5-main"},
	}})
	if err == nil || err.Error() != "duplicate model mapping: gpt-5 -> gpt-5-main" {
		t.Fatalf("unexpected duplicate error: %v", err)
	}
}

func TestNormalizeModelClosedWindowsNormalizesList(t *testing.T) {
	input := adminapi.ModelSelectionInput{Models: []adminapi.ModelMappingInput{
		{
			UpstreamName: "deepseek-flash",
			ClosedWindows: []adminapi.ModelClosedWindowInput{
				{TZ: "Asia/Shanghai", Weekdays: []int{5, 1}, Ranges: [][]string{{"14:00", "18:00"}, {"09:00", "12:00"}}},
				{TZ: "Asia/Shanghai", Weekdays: []int{6, 7}, Ranges: [][]string{{"00:00", "00:00"}}},
			},
		},
		// 只带时区的窗口等价于不限制，整条映射应被剔除并落成空串（清除）。
		{UpstreamName: "glm-5", ClosedWindows: []adminapi.ModelClosedWindowInput{{TZ: "Asia/Shanghai"}}},
		// 缺省字段：不进结果，保存时保持库里原值。
		{UpstreamName: "kimi-k2"},
	}}
	windows, err := normalizeModelClosedWindows(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 {
		t.Fatalf("应只有显式提交的 2 条映射进结果：%#v", windows)
	}
	key := modelMapping{UpstreamName: "deepseek-flash", PublicName: "deepseek-flash"}
	want := `[{"tz":"Asia/Shanghai","weekdays":[1,5],"ranges":[["14:00","18:00"],["09:00","12:00"]]},{"tz":"Asia/Shanghai","weekdays":[6,7],"ranges":[["00:00","00:00"]]}]`
	if windows[key] != want {
		t.Fatalf("多规则归一化结果异常：got %s, want %s", windows[key], want)
	}
	cleared := modelMapping{UpstreamName: "glm-5", PublicName: "glm-5"}
	if windows[cleared] != "" {
		t.Fatalf("全不限制的窗口应归一成空串表示清除，got %q", windows[cleared])
	}
}

func TestNormalizeModelClosedWindowsRejectsInvalidRule(t *testing.T) {
	input := adminapi.ModelSelectionInput{Models: []adminapi.ModelMappingInput{
		{UpstreamName: "deepseek-flash", ClosedWindows: []adminapi.ModelClosedWindowInput{
			{Weekdays: []int{8}, Ranges: [][]string{{"09:00", "12:00"}}},
		}},
	}}
	if _, err := normalizeModelClosedWindows(input); err == nil {
		t.Fatal("星期越界的规则应报错")
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
