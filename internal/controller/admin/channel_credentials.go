package admin

import (
	"strconv"

	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func (c *Controller) registerChannelCredentialRoutes(group *ghttp.RouterGroup) {
	group.GET("/channels/{id}/credentials", c.listChannelCredentials)
	group.POST("/channels/{id}/credentials", c.createChannelCredential)
	group.PUT("/channels/{id}/credentials/{credentialId}/status", c.updateChannelCredentialStatus)
	group.DELETE("/channels/{id}/credentials/{credentialId}", c.deleteChannelCredential)
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

func (c *Controller) deleteChannelCredential(r *ghttp.Request) {
	respond(r, map[string]any{}, c.channels.DeleteCredential(r.Context(), routeID(r), credentialRouteID(r)))
}

func credentialRouteID(r *ghttp.Request) uint64 {
	id, _ := strconv.ParseUint(r.GetRouter("credentialId").String(), 10, 64)
	return id
}
