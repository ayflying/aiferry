package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
)

func sanitizeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\r\n") {
		return "/"
	}
	return value
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", gerror.Wrap(err, "generate secure token")
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func stateKey(state string) string {
	return "aiferry:oauth-state:" + state
}

func sessionKey(token string) string {
	return "aiferry:admin-session:" + token
}
