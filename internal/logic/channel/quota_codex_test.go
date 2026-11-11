package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/model/entity"
)

func TestParseCodexUsage(t *testing.T) {
	view, err := parseQuotaResponse(channeltype.AdapterCodexUsage, []byte(`{
		"plan_type":"pro",
		"rate_limit":{"primary_window":{"used_percent":12.5,"limit_window_seconds":18000,"reset_at":1800000000,"reset_after_seconds":10},"secondary_window":{"used_percent":67,"limit_window_seconds":604800,"reset_after_seconds":120}},
		"code_review_rate_limit":{"primary_window":{"used_percent":3,"limit_window_seconds":604800,"reset_at":1800100000}},
		"credits":{"balance":"123.45","has_credits":true}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != channeltype.AdapterCodexUsage || view.Level != "pro" || len(view.Windows) != 3 {
		t.Fatalf("view = %+v", view)
	}
	primary, secondary, review := view.Windows[0], view.Windows[1], view.Windows[2]
	if primary.Kind != "codex_primary_18000" || primary.UsedPercent != 12.5 || primary.NextResetAt == nil || primary.NextResetAt.Unix() != 1800000000 {
		t.Fatalf("primary = %+v", primary)
	}
	if secondary.Kind != "codex_secondary_604800" || secondary.NextResetAt == nil || !secondary.NextResetAt.Equal(view.QueriedAt.Add(120*time.Second)) {
		t.Fatalf("secondary = %+v", secondary)
	}
	if review.Kind != "codex_code_review_primary_604800" || review.UsedPercent != 3 {
		t.Fatalf("code review = %+v", review)
	}
	for _, window := range view.Windows {
		if window.Used != nil || window.Total != nil || window.Remaining != nil || window.Counts {
			t.Fatalf("percentage must not turn into money/tokens: %+v", window)
		}
	}
}

func TestParseCodexUsageOptionalAndAdditionalWindows(t *testing.T) {
	view, err := parseCodexUsage([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":null,"secondary_window":{"used_percent":0,"limit_window_seconds":86400}},"additional_rate_limits":[{"limit_name":"Code review","metered_feature":"code_review","rate_limit":{"primary_window":{"used_percent":24,"limit_window_seconds":3600,"reset_after_seconds":0}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Windows) != 2 || view.Windows[0].Kind != "codex_secondary_86400" || view.Windows[0].UsedPercent != 0 || view.Windows[0].NextResetAt != nil {
		t.Fatalf("optional windows = %+v", view.Windows)
	}
	if view.Windows[1].Kind != "codex_feature_code_review_primary_3600" || view.Windows[1].NextResetAt == nil || !view.Windows[1].NextResetAt.Equal(view.QueriedAt) {
		t.Fatalf("additional window = %+v", view.Windows[1])
	}
}

func TestParseCodexUsageRejectsMissingWindows(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{}`, `{"rate_limit":null}`, `{"rate_limit":{"primary_window":{}}}`, `{"credits":{"balance":"100"}}`, `{"rate_limit":{"primary_window":{"used_percent":"50"}}}`} {
		if _, err := parseCodexUsage([]byte(body)); err == nil {
			t.Fatalf("must reject unavailable/invalid usage: %s", body)
		}
	}
}

func TestCodexWindowDurationAndMerge(t *testing.T) {
	for seconds, want := range map[int64]string{604800: "1 周", 86400: "1 天", 18000: "5 小时", 300: "5 分钟", 59: "59 秒"} {
		if got := codexWindowDuration(seconds); got != want {
			t.Fatalf("duration %d = %q, want %q", seconds, got, want)
		}
	}
	a, err := parseCodexUsage([]byte(`{"rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":18000},"secondary_window":{"used_percent":80,"limit_window_seconds":604800}}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseCodexUsage([]byte(`{"rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000},"secondary_window":{"used_percent":60,"limit_window_seconds":86400}}}`))
	if err != nil {
		t.Fatal(err)
	}
	merged := mergeQuotaViews([]QuotaView{a, b})
	if len(merged.Windows) != 3 || merged.Windows[0].UsedPercent != 30 {
		t.Fatalf("merge must separate different durations: %+v", merged.Windows)
	}
}

func TestFetchCodexQuotaUsesSharedUpstreamJSON(t *testing.T) {
	svc, secrets := newUpstreamAuthTestService(t, `{}`)
	cipher, err := secrets.Encrypt("fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/backend-api/wham/usage" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("unexpected quota request: %s %s %v", r.Method, r.URL, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":17,"limit_window_seconds":18000}}}`)
	}))
	defer server.Close()
	svc.app.HTTP = server.Client()
	cfg, err := channeltype.ParseConfig([]byte(`{"quota":{"adapter":"codex_usage"}}`))
	if err != nil {
		t.Fatal(err)
	}
	channel := entity.Channels{Type: "codex", BaseUrl: server.URL + "/backend-api/codex"}
	endpoint, err := resolveQuotaURL(channel.BaseUrl, cfg.Quota.Path)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.fetchQuotaWithCredential(context.Background(), channel, cfg.Quota, endpoint, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if view.Level != "plus" || len(view.Windows) != 1 || view.Windows[0].UsedPercent != 17 {
		t.Fatalf("view = %+v", view)
	}
}

func TestCodexQuotaURL(t *testing.T) {
	got, err := resolveQuotaURL("https://chatgpt.com/backend-api/codex", "/backend-api/wham/usage")
	if err != nil || got != "https://chatgpt.com/backend-api/wham/usage" {
		t.Fatalf("quota URL = %q, %v", got, err)
	}
}
