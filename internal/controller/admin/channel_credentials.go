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
	// 外链登录：先申请一次性登录地址，管理员在浏览器完成平台官方登录后再轮询换票，
	// 成功时令牌作为新的渠道凭据落库。
	group.POST("/channels/{id}/credentials/login", c.startChannelLogin)
	group.POST("/channels/{id}/credentials/login/poll", c.pollChannelLogin)
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

// startChannelLogin 申请一次性登录地址。登录动作在平台自己的登录页完成，
// 管理端拿到 authUrl 后展示给管理员（新窗口打开或扫码），再轮询 poll 接口。
func (c *Controller) startChannelLogin(r *ghttp.Request) {
	session, err := c.channels.StartChannelLogin(r.Context(), routeID(r))
	if err != nil {
		respond(r, nil, err)
		return
	}
	respond(r, adminapi.ChannelLoginSessionView{State: session.State, AuthURL: session.AuthURL}, nil)
}

// pollChannelLogin 轮询登录结果；status=completed 时凭据已写入该渠道。
func (c *Controller) pollChannelLogin(r *ghttp.Request) {
	var input adminapi.ChannelLoginPollInput
	if !parse(r, &input) {
		return
	}
	result, err := c.channels.PollChannelLogin(r.Context(), routeID(r), input.State)
	if err != nil {
		respond(r, nil, err)
		return
	}
	respond(r, adminapi.ChannelLoginResultView{
		Status:       result.Status,
		CredentialID: result.CredentialID,
		UID:          result.UID,
	}, nil)
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
