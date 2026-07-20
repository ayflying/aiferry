package relay

import (
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/yunloli/aiferry/internal/service/iplocation"
)

func clientIP(r *ghttp.Request) string {
	return clientIPFromHeaders(r.Header, r.GetClientIp())
}

func clientIPFromHeaders(headers http.Header, fallback string) string {
	for _, name := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
		for _, value := range headers.Values(name) {
			for _, candidate := range strings.Split(value, ",") {
				if ip := iplocation.NormalizeIP(candidate); ip != "" {
					return ip
				}
			}
		}
	}
	return iplocation.NormalizeIP(fallback)
}
