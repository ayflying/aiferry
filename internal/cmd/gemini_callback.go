package cmd

import (
	"context"
	"net/http"

	"github.com/gogf/gf/v2/net/ghttp"
)

func geminiSubscriptionCallback(accept func(context.Context, string, string, string) error) func(*ghttp.Request) {
	return func(r *ghttp.Request) {
		header := r.Response.Header()
		header.Set("Cache-Control", "no-store")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Type", "text/plain; charset=utf-8")
		header.Set("X-Content-Type-Options", "nosniff")
		q := r.URL.Query()
		if err := accept(r.Context(), q.Get("state"), q.Get("code"), q.Get("error")); err != nil {
			r.Response.WriteStatus(http.StatusBadRequest, "授权回调无效或已过期，请回到 AiFerry 重试。")
			return
		}
		r.Response.Write("授权回调已接收，请回到 AiFerry 查看登录结果。")
	}
}
