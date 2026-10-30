package cluster

import (
	"net/http"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// RegisterAdmin 注册管理端的集群节点接口。子节点不注册这些路由。
func (c *Controller) RegisterAdmin(group *ghttp.RouterGroup) {
	group.GET("/cluster/nodes", c.ListNodes)
	group.DELETE("/cluster/nodes/:id", c.DeleteNode)
}

// ListNodes 返回全部子节点及其配置版本是否落后。
func (c *Controller) ListNodes(r *ghttp.Request) {
	nodes, err := c.service.ListNodes(r.Context())
	if err != nil {
		g.Log().Warningf(r.Context(), "cluster: list nodes: %v", err)
		r.Response.WriteStatus(http.StatusInternalServerError, "failed to list cluster nodes")
		return
	}
	r.Response.WriteJson(map[string]any{"nodes": nodes})
}

// DeleteNode 吊销一个子节点。该节点随后会注册失败，需要重新下发密钥。
func (c *Controller) DeleteNode(r *ghttp.Request) {
	id := r.Get("id").Uint64()
	if id == 0 {
		r.Response.WriteStatus(http.StatusBadRequest, "invalid node id")
		return
	}
	if err := c.service.DeleteNode(r.Context(), id); err != nil {
		g.Log().Warningf(r.Context(), "cluster: delete node %d: %v", id, err)
		r.Response.WriteStatus(http.StatusInternalServerError, "failed to delete cluster node")
		return
	}
	r.Response.WriteJson(map[string]any{"ok": true})
}
