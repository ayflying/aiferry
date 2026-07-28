package upstreamerror

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

const maxMessageLength = 1024

// Message keeps the fields returned by an upstream error response readable
// without discarding provider-specific fields such as reason or code.
func Message(body []byte, fallback string) string {
	fallback = clean(fallback)
	if !gjson.ValidBytes(body) {
		return fallback
	}
	errorValue := gjson.GetBytes(body, "error")
	if !errorValue.Exists() {
		errorValue = gjson.ParseBytes(body)
	}
	if errorValue.IsObject() {
		fields := make([]string, 0)
		errorValue.ForEach(func(key, value gjson.Result) bool {
			name := clean(key.String())
			if name != "" {
				fields = append(fields, name+"="+formatValue(value))
			}
			return true
		})
		if len(fields) > 0 {
			return trim("error: " + strings.Join(fields, " "))
		}
	}
	if message := clean(errorValue.String()); message != "" {
		return trim("error: message=" + strconv.Quote(message))
	}
	return fallback
}

func formatValue(value gjson.Result) string {
	switch value.Type {
	case gjson.String:
		return strconv.Quote(clean(value.String()))
	case gjson.Number, gjson.True, gjson.False, gjson.Null:
		return value.Raw
	default:
		return trim(clean(value.Raw))
	}
}

func clean(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	for _, marker := range []string{"Bearer ", "bearer ", "sk-"} {
		if index := strings.Index(value, marker); index >= 0 {
			value = value[:index] + marker + "***"
		}
	}
	return value
}

func trim(value string) string {
	if len(value) <= maxMessageLength {
		return value
	}
	return value[:maxMessageLength-3] + "..."
}
