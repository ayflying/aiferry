package relay

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

const (
	redactedPassword = "[REDACTED_PASSWORD]"
	redactedToken    = "[REDACTED_TOKEN]"
	redactedPersonal = "[REDACTED_PERSONAL_DATA]"
)

var (
	assignmentPattern      = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])([A-Za-z][A-Za-z0-9_-]*)(\s*(?:=|:)\s*)((?:bearer\s+)?[^\s,;]+)`)
	chinesePasswordPattern = regexp.MustCompile(`密码\s*(?:为|是|=|:)\s*[^\s,;]+`)
	basicAuthURLPattern    = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/\s:@]+:)([^@/\s]+)(@)`)
	bearerTokenPattern     = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	commonTokenPattern     = regexp.MustCompile(`\b(?:sk|rk|pk)-[a-zA-Z0-9_-]{8,}\b|\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	privateKeyPattern      = regexp.MustCompile(`(?is)-----BEGIN(?: [A-Z0-9]+)? PRIVATE KEY-----.*?-----END(?: [A-Z0-9]+)? PRIVATE KEY-----`)
	emailPattern           = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
	mainlandPhonePattern   = regexp.MustCompile(`(?:\+?86[- ]?)?1[3-9]\d{9}`)
	chineseIDPattern       = regexp.MustCompile(`\b[1-9]\d{16}[\dXx]\b`)
	bankCardPattern        = regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)
)

func redactSensitiveData(body []byte, settings adminapi.SensitiveWordSettingsInput) ([]byte, error) {
	if !settings.SensitiveDataRedactionEnabled {
		return body, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, gerror.Wrap(err, "decode request for sensitive data redaction")
	}
	result, err := json.Marshal(redactSensitiveValue(payload, settings))
	if err != nil {
		return nil, gerror.Wrap(err, "encode redacted request")
	}
	return result, nil
}

func redactSensitiveValue(value any, settings adminapi.SensitiveWordSettingsInput) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if placeholder, shouldReplace := sensitiveFieldPlaceholder(key, item, settings); shouldReplace {
				typed[key] = placeholder
				continue
			}
			typed[key] = redactSensitiveValue(item, settings)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = redactSensitiveValue(item, settings)
		}
		return typed
	case string:
		return redactSensitiveText(typed, settings)
	default:
		return value
	}
}

func sensitiveFieldPlaceholder(key string, value any, settings adminapi.SensitiveWordSettingsInput) (string, bool) {
	if !isSensitiveScalar(value) {
		return "", false
	}
	normalized := normalizeSensitiveKey(key)
	switch {
	case settings.PasswordRedactionEnabled && isPasswordField(normalized):
		return redactedPassword, true
	case settings.TokenRedactionEnabled && isTokenField(normalized):
		return redactedToken, true
	case settings.PersonalDataRedactionEnabled && (strings.Contains(normalized, "email") || strings.Contains(normalized, "phone") || strings.Contains(normalized, "mobile") || strings.Contains(normalized, "idcard") || strings.Contains(normalized, "identity") || strings.Contains(normalized, "bankcard")):
		return redactedPersonal, true
	default:
		return "", false
	}
}

func normalizeSensitiveKey(value string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.TrimSpace(value)))
}

func isPasswordField(value string) bool {
	return strings.Contains(value, "password") || value == "passwd" || value == "pwd"
}

func isTokenField(value string) bool {
	switch value {
	case "token", "accesstoken", "refreshtoken", "idtoken", "bearertoken", "authtoken", "authorization", "auth", "cookie", "apikey", "accesskey", "privatekey", "secret", "clientsecret", "secretkey":
		return true
	}
	if strings.HasSuffix(value, "apikey") || strings.HasSuffix(value, "accesskey") || strings.HasSuffix(value, "secret") || strings.HasSuffix(value, "secretkey") {
		return true
	}
	if !strings.HasSuffix(value, "token") {
		return false
	}
	for _, prefix := range []string{"max", "input", "output", "cached", "total", "prompt", "completion", "reasoning"} {
		if strings.HasPrefix(value, prefix) {
			return false
		}
	}
	return true
}

func isSensitiveScalar(value any) bool {
	switch value.(type) {
	case string, json.Number, float64, bool:
		return true
	default:
		return false
	}
}

func redactSensitiveText(value string, settings adminapi.SensitiveWordSettingsInput) string {
	if settings.PasswordRedactionEnabled {
		value = redactAssignments(value, isPasswordField, redactedPassword)
		value = chinesePasswordPattern.ReplaceAllString(value, "密码="+redactedPassword)
		value = basicAuthURLPattern.ReplaceAllString(value, "${1}"+redactedPassword+"${3}")
	}
	if settings.TokenRedactionEnabled {
		value = privateKeyPattern.ReplaceAllString(value, redactedToken)
		value = redactAssignments(value, isTokenField, redactedToken)
		value = bearerTokenPattern.ReplaceAllString(value, "Bearer "+redactedToken)
		value = commonTokenPattern.ReplaceAllString(value, redactedToken)
	}
	if settings.PersonalDataRedactionEnabled {
		value = emailPattern.ReplaceAllString(value, redactedPersonal)
		value = redactValidatedMatches(value, chineseIDPattern, validChineseID, redactedPersonal)
		value = redactValidatedMatches(value, bankCardPattern, validBankCard, redactedPersonal)
		value = mainlandPhonePattern.ReplaceAllString(value, redactedPersonal)
	}
	return value
}

func redactAssignments(value string, matches func(string) bool, replacement string) string {
	return assignmentPattern.ReplaceAllStringFunc(value, func(assignment string) string {
		parts := assignmentPattern.FindStringSubmatch(assignment)
		if len(parts) != 5 || !matches(normalizeSensitiveKey(parts[2])) {
			return assignment
		}
		return parts[1] + parts[2] + parts[3] + replacement
	})
}

func redactValidatedMatches(value string, pattern *regexp.Regexp, valid func(string) bool, replacement string) string {
	locations := pattern.FindAllStringIndex(value, -1)
	if len(locations) == 0 {
		return value
	}
	var (
		builder strings.Builder
		last    int
		changed bool
	)
	for _, location := range locations {
		matched := value[location[0]:location[1]]
		if !valid(matched) {
			continue
		}
		builder.WriteString(value[last:location[0]])
		builder.WriteString(replacement)
		last = location[1]
		changed = true
	}
	if !changed {
		return value
	}
	builder.WriteString(value[last:])
	return builder.String()
}

func validChineseID(value string) bool {
	if len(value) != 18 {
		return false
	}
	weights := [...]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	checks := "10X98765432"
	sum := 0
	for index, weight := range weights {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
		sum += int(value[index]-'0') * weight
	}
	return strings.EqualFold(string(value[17]), string(checks[sum%11]))
}

func validBankCard(value string) bool {
	digits := make([]byte, 0, len(value))
	for index := range value {
		if value[index] >= '0' && value[index] <= '9' {
			digits = append(digits, value[index])
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for index := len(digits) - 1; index >= 0; index-- {
		digit := int(digits[index] - '0')
		if (len(digits)-1-index)%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}
