package channel

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/gogf/gf/v2/errors/gerror"
)

func TestCredentialInsertErrorDoesNotExposeSQL(t *testing.T) {
	duplicate := &mysql.MySQLError{Number: 1062, Message: "secret SQL"}
	for _, err := range []error{duplicate, fmt.Errorf("wrapped: %w", duplicate), gerror.Wrap(duplicate, "INSERT secret")} {
		if got := credentialInsertError(err).Error(); got != "该渠道已添加相同密钥" {
			t.Fatalf("unexpected duplicate message: %s", got)
		}
	}
	got := credentialInsertError(fmt.Errorf("INSERT secret cipher")).Error()
	if strings.Contains(got, "secret") || got != "保存上游密钥失败，请稍后重试" {
		t.Fatalf("unsafe database message: %s", got)
	}
}
