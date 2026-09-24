package channel

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/yunloli/aiferry/internal/model/entity"
)

func TestChannelAutoDisableEnabledDefaultsAndOverrides(t *testing.T) {
	if !channelAutoDisableEnabled(nil, true) || channelAutoDisableEnabled(nil, false) {
		t.Fatal("missing value should preserve the supplied default")
	}
	falseValue := false
	trueValue := true
	if channelAutoDisableEnabled(&trueValue, false) != true || channelAutoDisableEnabled(&falseValue, true) != false {
		t.Fatal("explicit value should override the supplied default")
	}
}

func TestSelectHealthCheckModelIDUsesConfiguredModelOrEnabledFallback(t *testing.T) {
	models := []entity.ChannelModels{{Id: 3}, {Id: 7}}
	if got := selectHealthCheckModelID(0, models); got != 3 {
		t.Fatalf("fallback model = %d, want 3", got)
	}
	if got := selectHealthCheckModelID(7, models); got != 7 {
		t.Fatalf("configured model = %d, want 7", got)
	}
	if got := selectHealthCheckModelID(9, models); got != 0 {
		t.Fatalf("missing configured model = %d, want 0", got)
	}
}

// 用户可见错误必须是中文可读提示，不能把 sql no rows 等技术细节抛到弹框。
func TestHealthCheckModelLookupErrorUsesChineseMessage(t *testing.T) {
	if err := healthCheckModelLookupError(sql.ErrNoRows, entity.ChannelModels{}); err == nil {
		t.Fatal("no rows must be reported as missing test model")
	} else {
		got := err.Error()
		if !containsChinese(got) || strings.Contains(got, "no rows") || strings.Contains(got, "find channel test model") {
			t.Fatalf("no rows message must be Chinese without SQL detail, got %q", got)
		}
		if !strings.Contains(got, "测试模型") {
			t.Fatalf("no rows message must mention 测试模型, got %q", got)
		}
	}

	if err := healthCheckModelLookupError(nil, entity.ChannelModels{}); err == nil {
		t.Fatal("empty model id must be reported as missing test model")
	} else {
		got := err.Error()
		if strings.Contains(got, "enabled model") || !strings.Contains(got, "测试模型") {
			t.Fatalf("empty model message must be Chinese, got %q", got)
		}
	}

	dbErr := errors.New("connection refused")
	if err := healthCheckModelLookupError(dbErr, entity.ChannelModels{Id: 5}); err == nil {
		t.Fatal("real database error must not be ignored")
	} else {
		got := err.Error()
		if !strings.Contains(got, "查询测试模型失败") || !containsChinese(got) {
			t.Fatalf("database error must be wrapped with Chinese prefix, got %q", got)
		}
	}

	if err := healthCheckModelLookupError(nil, entity.ChannelModels{Id: 5}); err != nil {
		t.Fatalf("valid model must pass, got %v", err)
	}
}

func containsChinese(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
