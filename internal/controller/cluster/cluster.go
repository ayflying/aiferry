package cluster

import (
	"encoding/json"
	"net/http"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/yunloli/aiferry/internal/logic/cluster"
)

// Controller 暴露节点间内部接口。这些接口只对持有集群密钥的节点开放，
// 通过 HMAC 签名完成身份认证，不走管理端的 Casdoor 会话。
type Controller struct {
	service *cluster.Service
}

// New 构造内部接口控制器。
func New(service *cluster.Service) *Controller {
	return &Controller{service: service}
}

// RegisterForMaster 注册主节点侧接口：子节点注册、拉取快照、上报心跳、回传用量，
// 以及子节点主动订阅的 WS 通知通道。
func (c *Controller) RegisterForMaster(group *ghttp.RouterGroup) {
	group.POST("/register", c.Register)
	group.POST("/config", c.Config)
	group.POST("/heartbeat", c.Heartbeat)
	group.POST("/usage", c.Usage)
	group.GET("/stream", c.Stream)
}

// Stream 把已验签的 GET 升级成 WS 长连接，用于向子节点即时下发配置变更。
//
// 为什么必须放在主节点：子节点可能部署在内网，主节点无法主动连进去，
// 只能由子节点反向建链。端口与 HTTP 完全共用，不需要额外开放。
//
// 验签在升级之前完成：升级后 GoFrame 已交出连接，中间件不再生效。
func (c *Controller) Stream(r *ghttp.Request) {
	ctx := r.Context()
	name, err := c.service.VerifiedNodeName(ctx, r.Request, nil)
	if err != nil {
		writeUnauthorized(r, err)
		return
	}
	// 只有已注册且未吊销的节点才允许订阅，吊销后连接会在下一次重连时被拒。
	if _, err = c.service.FindNodeByName(ctx, name); err != nil {
		writeUnauthorized(r, err)
		return
	}
	if err = c.service.ServeStream(ctx, r.Response.Writer, r.Request, name, r.Header.Get("X-Aiferry-Version")); err != nil {
		g.Log().Warningf(ctx, "cluster: upgrade stream for node %s: %v", name, err)
	}
}

// RegisterForWorker 注册子节点侧接口：只接收主节点的配置变更通知。
func (c *Controller) RegisterForWorker(group *ghttp.RouterGroup) {
	group.POST("/notify", c.Notify)
}

// Register 处理子节点注册。
func (c *Controller) Register(r *ghttp.Request) {
	ctx := r.Context()
	body := r.GetBody()
	name, err := c.service.VerifiedNodeName(ctx, r.Request, body)
	if err != nil {
		writeUnauthorized(r, err)
		return
	}
	request := struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Address string `json:"address"`
	}{}
	if err = json.Unmarshal(body, &request); err != nil {
		r.Response.WriteStatus(http.StatusBadRequest, "invalid register body")
		return
	}
	if request.Name != name {
		r.Response.WriteStatus(http.StatusUnauthorized, "cluster: signed node does not match body")
		return
	}
	reply, err := c.service.HandleRegister(ctx, cluster.RegisterInput{
		Name:       request.Name,
		Address:    request.Address,
		Version:    request.Version,
		RemoteAddr: r.RemoteAddr,
	})
	if err != nil {
		g.Log().Warningf(ctx, "cluster: register node %s: %v", name, err)
		r.Response.WriteStatus(http.StatusBadRequest, err.Error())
		return
	}
	r.Response.WriteJson(reply)
}

// Config 返回全量配置快照。版本未变化时以 304 结束，避免重复传输。
func (c *Controller) Config(r *ghttp.Request) {
	ctx := r.Context()
	body := r.GetBody()
	if _, err := c.service.Authorize(ctx, r.Request, body); err != nil {
		writeUnauthorized(r, err)
		return
	}
	since, err := cluster.ParseQueryInt(r.Get("version").String())
	if err != nil {
		r.Response.WriteStatus(http.StatusBadRequest, err.Error())
		return
	}
	payload, notModified, err := c.service.ConfigPayload(ctx, since, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		g.Log().Warningf(ctx, "cluster: build config snapshot: %v", err)
		r.Response.WriteStatus(http.StatusInternalServerError, "failed to build config snapshot")
		return
	}
	if notModified {
		r.Response.WriteStatus(http.StatusNotModified)
		return
	}
	r.Response.Header().Set("Content-Type", "application/json")
	r.Response.Header().Set("Content-Encoding", "gzip")
	r.Response.Write(payload)
}

// Heartbeat 记录子节点的运行时状态。
func (c *Controller) Heartbeat(r *ghttp.Request) {
	ctx := r.Context()
	body := r.GetBody()
	node, err := c.service.Authorize(ctx, r.Request, body)
	if err != nil {
		writeUnauthorized(r, err)
		return
	}
	if err = c.service.HandleHeartbeat(ctx, node, body); err != nil {
		r.Response.WriteStatus(http.StatusBadRequest, err.Error())
		return
	}
	r.Response.WriteStatus(http.StatusOK)
}

// Usage 接收子节点回传的用量明细并完成记账。
func (c *Controller) Usage(r *ghttp.Request) {
	ctx := r.Context()
	body := r.GetBody()
	node, err := c.service.Authorize(ctx, r.Request, body)
	if err != nil {
		writeUnauthorized(r, err)
		return
	}
	ack, err := c.service.HandleUsageBody(ctx, node, body)
	if err != nil {
		g.Log().Warningf(ctx, "cluster: ingest usage from node %s: %v", node.Name, err)
		r.Response.WriteStatus(http.StatusBadRequest, err.Error())
		return
	}
	r.Response.Header().Set("Content-Type", "application/json")
	r.Response.Header().Set("Content-Encoding", "gzip")
	payload, err := cluster.EncodeGzipJSON(ack)
	if err != nil {
		r.Response.WriteStatus(http.StatusInternalServerError, "failed to encode usage ack")
		return
	}
	r.Response.Write(payload)
}

// Notify 接收主节点的配置变更推送，唤醒一次立即同步。
func (c *Controller) Notify(r *ghttp.Request) {
	ctx := r.Context()
	body := r.GetBody()
	if err := c.service.AuthorizeMaster(ctx, r.Request, body); err != nil {
		writeUnauthorized(r, err)
		return
	}
	request := struct {
		Version int64 `json:"version"`
	}{}
	if err := json.Unmarshal(body, &request); err != nil {
		r.Response.WriteStatus(http.StatusBadRequest, "invalid notify body")
		return
	}
	if err := c.service.NotifyConfigVersion(ctx, request.Version); err != nil {
		g.Log().Warningf(ctx, "cluster: apply pushed version %d: %v", request.Version, err)
		r.Response.WriteStatus(http.StatusInternalServerError, err.Error())
		return
	}
	r.Response.WriteStatus(http.StatusNoContent)
}

func writeUnauthorized(r *ghttp.Request, err error) {
	r.Response.WriteStatus(http.StatusUnauthorized, err.Error())
}
