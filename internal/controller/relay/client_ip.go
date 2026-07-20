package relay

import (
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/yunloli/aiferry/internal/service/iplocation"
)

func clientIP(r *ghttp.Request) string {
	return clientIPFromHeaders(r.Header, r.GetClientIp(), r.GetRemoteIp(), r.RemoteAddr)
}

func clientIPFromHeaders(headers http.Header, fallbacks ...string) string {
	for _, name := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
		for _, value := range headers.Values(name) {
			for _, candidate := range strings.Split(value, ",") {
				if ip := iplocation.NormalizeIP(candidate); ip != "" {
					return ip
				}
			}
		}
	}
	for _, fallback := range fallbacks {
		if ip := iplocation.NormalizeIP(fallback); ip != "" {
			return ip
		}
	}
	return ""
}
