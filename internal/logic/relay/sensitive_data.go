package relay

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

var (
	assignmentPattern      = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])([A-Za-z][A-Za-z0-9_-]*)(\s*(?:=|:)\s*)((?:bearer\s+)?[^\s,;]+)`)
	chinesePasswordPattern = regexp.MustCompile(`密码\s*(?:为|是|=|:)\s*([^\s,;]+)`)
	basicAuthURLPattern    = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/\s:@]+:)([^@/\s]+)(@)`)
	bearerTokenPattern     = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	commonTokenPattern     = regexp.MustCompile(`\b(?:sk|rk|pk)-[a-zA-Z0-9_-]{8,}\b|\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	privateKeyPattern      = regexp.MustCompile(`(?is)-----BEGIN(?: [A-Z0-9]+)? PRIVATE KEY-----.*?-----END(?: [A-Z0-9]+)? PRIVATE KEY-----`)
	emailPattern           = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
	mainlandPhonePattern   = regexp.MustCompile(`(?:\+?86[- ]?)?1[3-9]\d{9}`)
	chineseIDPattern       = regexp.MustCompile(`\b[1-9]\d{16}[\dXx]\b`)
	bankCardPattern        = regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)
)

type sensitiveDataRestorer struct {
	requestID    string
	replacements map[string]string
	nextID       uint64
}

func redactSensitiveData(body []byte, settings adminapi.SensitiveWordSettingsInput) ([]byte, error) {
	redacted, _, err := redactSensitiveDataWithRestore(body, settings)
	return redacted, err
}

func redactSensitiveDataWithRestore(body []byte, settings adminapi.SensitiveWordSettingsInput) ([]byte, *sensitiveDataRestorer, error) {
	if !settings.SensitiveDataRedactionEnabled {
		return body, nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, gerror.Wrap(err, "decode request for sensitive data redaction")
	}
	restorer, err := newSensitiveDataRestorer()
	if err != nil {
		return nil, nil, err
	}
	redacted := redactSensitiveValue(payload, settings, restorer)
	if len(restorer.replacements) == 0 {
		return body, nil, nil
	}
	result, err := json.Marshal(redacted)
	if err != nil {
		return nil, nil, gerror.Wrap(err, "encode redacted request")
	}
	return result, restorer, nil
}

func newSensitiveDataRestorer() (*sensitiveDataRestorer, error) {
	seed := make([]byte, 16)
	if _, err := rand.Read(seed); err != nil {
		return nil, gerror.Wrap(err, "create sensitive data placeholder")
	}
	return &sensitiveDataRestorer{
		requestID:    hex.EncodeToString(seed),
		replacements: make(map[string]string),
	}, nil
}

func (r *sensitiveDataRestorer) redact(value string) string {
	r.nextID++
	// Keep the value opaque without syntax that upstream filters may treat specially.
	placeholder := fmt.Sprintf("aiferry_ref_%s_%d_", r.requestID, r.nextID)
	r.replacements[placeholder] = value
	return placeholder
}

func (r *sensitiveDataRestorer) redactTextMatch(value string) string {
	for placeholder := range r.replacements {
		if strings.Contains(value, placeholder) {
			return value
		}
	}
	return r.redact(value)
}

func redactSensitiveValue(value any, settings adminapi.SensitiveWordSettingsInput, restorer *sensitiveDataRestorer) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if shouldReplace := sensitiveFieldShouldRedact(key, item, settings); shouldReplace {
				typed[key] = restorer.redact(sensitiveScalarString(item))
				continue
			}
			typed[key] = redactSensitiveValue(item, settings, restorer)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = redactSensitiveValue(item, settings, restorer)
		}
		return typed
	case string:
		return redactSensitiveText(typed, settings, restorer)
	default:
		return value
	}
}

func sensitiveFieldShouldRedact(key string, value any, settings adminapi.SensitiveWordSettingsInput) bool {
	if !isSensitiveScalar(value) {
		return false
	}
	normalized := normalizeSensitiveKey(key)
	switch {
	case settings.PasswordRedactionEnabled && isPasswordField(normalized):
		return true
	case settings.TokenRedactionEnabled && isTokenField(normalized):
		return true
	case settings.PersonalDataRedactionEnabled && (strings.Contains(normalized, "email") || strings.Contains(normalized, "phone") || strings.Contains(normalized, "mobile") || strings.Contains(normalized, "idcard") || strings.Contains(normalized, "identity") || strings.Contains(normalized, "bankcard")):
		return true
	default:
		return false
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

func sensitiveScalarString(value any) string {
	return fmt.Sprint(value)
}

func redactSensitiveText(value string, settings adminapi.SensitiveWordSettingsInput, restorer *sensitiveDataRestorer) string {
	redactMatch := restorer.redactTextMatch
	if settings.PasswordRedactionEnabled {
		value = redactAssignments(value, isPasswordField, redactMatch)
		value = chinesePasswordPattern.ReplaceAllStringFunc(value, func(match string) string {
			parts := chinesePasswordPattern.FindStringSubmatch(match)
			return "密码=" + redactMatch(parts[1])
		})
		value = basicAuthURLPattern.ReplaceAllStringFunc(value, func(match string) string {
			parts := basicAuthURLPattern.FindStringSubmatch(match)
			return parts[1] + redactMatch(parts[2]) + parts[3]
		})
	}
	if settings.TokenRedactionEnabled {
		value = privateKeyPattern.ReplaceAllStringFunc(value, redactMatch)
		value = redactAssignments(value, isTokenField, redactMatch)
		value = bearerTokenPattern.ReplaceAllStringFunc(value, func(match string) string {
			return "Bearer " + redactMatch(strings.TrimSpace(match[len("Bearer "):]))
		})
		value = commonTokenPattern.ReplaceAllStringFunc(value, redactMatch)
	}
	if settings.PersonalDataRedactionEnabled {
		value = emailPattern.ReplaceAllStringFunc(value, redactMatch)
		value = redactValidatedMatches(value, chineseIDPattern, validChineseID, redactMatch)
		value = redactValidatedMatches(value, bankCardPattern, validBankCard, redactMatch)
		value = mainlandPhonePattern.ReplaceAllStringFunc(value, redactMatch)
	}
	return value
}

func redactAssignments(value string, matches func(string) bool, redact func(string) string) string {
	return assignmentPattern.ReplaceAllStringFunc(value, func(assignment string) string {
		parts := assignmentPattern.FindStringSubmatch(assignment)
		if len(parts) != 5 || !matches(normalizeSensitiveKey(parts[2])) {
			return assignment
		}
		return parts[1] + parts[2] + parts[3] + redact(parts[4])
	})
}

func redactValidatedMatches(value string, pattern *regexp.Regexp, valid func(string) bool, redact func(string) string) string {
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
		builder.WriteString(redact(matched))
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
