package admin

import (
	"strconv"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/auth"
)

func (c *Controller) registerChannelCredentialRoutes(group *ghttp.RouterGroup) {
	group.GET("/channels/{id}/credentials", c.listChannelCredentials)
	group.POST("/channels/{id}/credentials", c.createChannelCredential)
	group.PUT("/channels/{id}/credentials/{credentialId}/status", c.updateChannelCredentialStatus)
	group.PUT("/channels/{id}/credentials/{credentialId}/management-key", c.updateChannelCredentialManagementKey)
	group.DELETE("/channels/{id}/credentials/{credentialId}", c.deleteChannelCredential)
	// 查看上游密钥明文：先过 10 分钟邮箱验证窗口；配套发码/验码/状态端点。
	group.GET("/channels/{id}/credentials/{credentialId}/secret", c.revealChannelCredential)
	group.GET("/credential-reveal/status", c.credentialRevealStatus)
	group.POST("/credential-reveal/code", c.sendCredentialRevealCode)
	group.POST("/credential-reveal/verify", c.verifyCredentialRevealCode)
}

// revealChannelCredential 返回上游密钥明文。权威门是邮箱验证后的 10 分钟
// 窗口（Redis TTL）；未验证时前端先走 status/verify 流程，这里兜底拦截。
func (c *Controller) revealChannelCredential(r *ghttp.Request) {
	r.Response.Header().Set("Cache-Control", "no-store")
	current, ok := auth.CurrentUser(r.Context())
	if !ok {
		respond(r, nil, auth.ErrUnauthorized)
		return
	}
	verified, err := c.mail.CredentialRevealVerified(r.Context(), current.Id)
	if err != nil {
		respond(r, nil, err)
		return
	}
	if !verified {
		respond(r, nil, gerror.New("需要邮箱验证后才能查看密钥，请先完成验证"))
		return
	}
	key, err := c.channels.RevealCredential(r.Context(), routeID(r), credentialRouteID(r))
	respond(r, map[string]string{"key": key}, err)
}

func (c *Controller) credentialRevealStatus(r *ghttp.Request) {
	current, ok := auth.CurrentUser(r.Context())
	if !ok {
		respond(r, nil, auth.ErrUnauthorized)
		return
	}
	data, err := c.mail.CredentialRevealStatus(r.Context(), current.Id)
	respond(r, data, err)
}

func (c *Controller) sendCredentialRevealCode(r *ghttp.Request) {
	current, ok := auth.CurrentUser(r.Context())
	if !ok {
		respond(r, nil, auth.ErrUnauthorized)
		return
	}
	data, err := c.mail.SendCredentialRevealCode(r.Context(), current.Id)
	respond(r, data, err)
}

func (c *Controller) verifyCredentialRevealCode(r *ghttp.Request) {
	var input adminapi.CredentialRevealVerifyInput
	if !parse(r, &input) {
		return
	}
	current, ok := auth.CurrentUser(r.Context())
	if !ok {
		respond(r, nil, auth.ErrUnauthorized)
		return
	}
	if err := c.mail.VerifyCredentialRevealCode(r.Context(), current.Id, input.Code); err != nil {
		respond(r, nil, err)
		return
	}
	data, statusErr := c.mail.CredentialRevealStatus(r.Context(), current.Id)
	respond(r, data, statusErr)
}

func (c *Controller) listChannelCredentials(r *ghttp.Request) {
	data, err := c.channels.ListCredentials(r.Context(), routeID(r))
	respond(r, data, err)
}

func (c *Controller) createChannelCredential(r *ghttp.Request) {
	var input adminapi.ChannelCredentialInput
	if !parse(r, &input) {
		return
	}
	id, err := c.channels.CreateCredential(r.Context(), routeID(r), input)
	respond(r, map[string]uint64{"id": id}, err)
}

func (c *Controller) updateChannelCredentialStatus(r *ghttp.Request) {
	var input adminapi.ChannelCredentialStatusInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.channels.SetCredentialStatus(r.Context(), routeID(r), credentialRouteID(r), input))
}

func (c *Controller) updateChannelCredentialManagementKey(r *ghttp.Request) {
	var input adminapi.ChannelCredentialManagementKeyInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.channels.SetCredentialManagementKey(r.Context(), routeID(r), credentialRouteID(r), input))
}

func (c *Controller) deleteChannelCredential(r *ghttp.Request) {
	respond(r, map[string]any{}, c.channels.DeleteCredential(r.Context(), routeID(r), credentialRouteID(r)))
}

func credentialRouteID(r *ghttp.Request) uint64 {
	id, _ := strconv.ParseUint(r.GetRouter("credentialId").String(), 10, 64)
	return id
}
