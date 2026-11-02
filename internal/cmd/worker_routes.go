package cmd

import (
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

// workerLocalRoutes 禁止把节点本地出站队列误当完整账单、或创建无法跨节点追踪的异步任务。
// 这些端点须直接访问主节点；不为它们增加逐请求访问主节点的隐式代理。
func workerLocalRoutes(r *ghttp.Request) {
	path := strings.TrimRight(r.URL.Path, "/")
	if workerRouteRequiresMaster(path) {
		r.Response.Header().Set("Content-Type", "application/json")
		r.Response.WriteStatus(http.StatusNotImplemented, `{"error":{"type":"master_node_required","message":"此端点需要主节点的完整账务或异步任务状态，请直接访问主节点"}}`)
		r.Exit()
		return
	}
	r.Middleware.Next()
}

func workerRouteRequiresMaster(path string) bool {
	return path == "/v1/dashboard/billing/usage" || path == "/v1/dashboard/billing/channels" ||
		path == "/v1/video" || strings.HasPrefix(path, "/v1/video/") ||
		path == "/v1/videos" || strings.HasPrefix(path, "/v1/videos/")
}
