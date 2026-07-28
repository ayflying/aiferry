package relay

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const redactedGatewayHost = "redacted.invalid"

func redactGatewayRequest(body []byte, headers http.Header, gatewayHost string) ([]byte, http.Header) {
	return []byte(redactGatewayText(string(body), gatewayHost)), redactGatewayHeaders(headers, gatewayHost)
}

func redactGatewayHeaders(headers http.Header, gatewayHost string) http.Header {
	result := headers.Clone()
	for _, name := range []string{
		"Host", "Origin", "Referer", "Forwarded", "Via",
		"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-IP", "CF-Connecting-IP",
	} {
		result.Del(name)
	}
	for name, values := range result {
		result.Del(name)
		for _, value := range values {
			result.Add(name, redactGatewayText(value, gatewayHost))
		}
	}
	return result
}

func redactGatewayText(value, gatewayHost string) string {
	for _, host := range gatewayHostVariants(gatewayHost) {
		value = replaceAllFold(value, host, redactedGatewayHost)
	}
	return value
}

func gatewayHostVariants(gatewayHost string) []string {
	gatewayHost = strings.TrimSpace(gatewayHost)
	if gatewayHost == "" {
		return nil
	}
	values := []string{gatewayHost}
	if parsed, err := url.Parse("//" + gatewayHost); err == nil && parsed.Hostname() != "" {
		values = append(values, parsed.Hostname())
	}
	unique := make(map[string]string, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		unique[strings.ToLower(value)] = value
	}
	result := make([]string, 0, len(unique))
	for _, value := range unique {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return len(result[i]) > len(result[j]) })
	return result
}

func replaceAllFold(value, target, replacement string) string {
	if target == "" {
		return value
	}
	matcher, err := regexp.Compile("(?i)" + regexp.QuoteMeta(target))
	if err != nil {
		return value
	}
	return matcher.ReplaceAllString(value, replacement)
}
