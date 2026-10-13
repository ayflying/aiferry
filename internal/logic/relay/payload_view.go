package relay

import (
	"encoding/json"
	"regexp"
	"strings"
)

var payloadSecretText = regexp.MustCompile(`(?i)(bearer\s+)[^\s"<>]+|\bsk-[A-Za-z0-9_-]+`)

// SanitizePayload 不返回鉴权凭据；普通用户也不获得平台渠道名称。
// 使用 JSON 解码再编码，避免字符串替换破坏报文结构。
func SanitizePayload(data []byte, admin bool) ([]byte, error) {
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if !admin {
		delete(value, "channelName")
	}
	return json.Marshal(sanitizePayloadValue(value))
}

func sanitizePayloadValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(key))
			switch normalized {
			case "authorization", "proxyauthorization", "apikey", "xapikey", "accesskey", "secretkey", "accesstoken", "refreshtoken", "password", "secret", "cookie", "setcookie", "managementkey", "apikeycipher", "managementkeycipher":
				current[key] = "[REDACTED]"
			default:
				current[key] = sanitizePayloadValue(item)
			}
		}
	case []any:
		for index := range current {
			current[index] = sanitizePayloadValue(current[index])
		}
	case string:
		return payloadSecretText.ReplaceAllString(current, "[REDACTED]")
	}
	return value
}
