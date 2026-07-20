package admin

import (
	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/service/channeltype"
)

func (c *Controller) listChannelGroups(r *ghttp.Request) {
	data, err := c.groups.List(r.Context())
	respond(r, data, err)
}

func (c *Controller) createChannelGroup(r *ghttp.Request) {
	var input adminapi.ChannelGroupInput
	if !parse(r, &input) {
		return
	}
	id, err := c.groups.Create(r.Context(), input)
	c.respondAfterChannelListMutation(r, map[string]any{"id": id}, err)
}

func (c *Controller) updateChannelGroup(r *ghttp.Request) {
	var input adminapi.ChannelGroupInput
	if !parse(r, &input) {
		return
	}
	c.respondAfterChannelListMutation(r, map[string]any{}, c.groups.Update(r.Context(), routeID(r), input))
}

func (c *Controller) deleteChannelGroup(r *ghttp.Request) {
	c.respondAfterChannelListMutation(r, map[string]any{}, c.groups.Delete(r.Context(), routeID(r)))
}

func (c *Controller) listChannelTypes(r *ghttp.Request) {
	data, err := c.types.List(r.Context())
	respond(r, data, err)
}

func (c *Controller) defaultChannelTypeConfig(r *ghttp.Request) {
	respond(r, channeltype.DefaultConfig(), nil)
}

func (c *Controller) createChannelType(r *ghttp.Request) {
	var input adminapi.ChannelTypeInput
	if !parse(r, &input) {
		return
	}
	id, err := c.types.Create(r.Context(), input)
	c.respondAfterChannelListMutation(r, map[string]any{"id": id}, err)
}

func (c *Controller) updateChannelType(r *ghttp.Request) {
	var input adminapi.ChannelTypeInput
	if !parse(r, &input) {
		return
	}
	c.respondAfterChannelListMutation(r, map[string]any{}, c.types.Update(r.Context(), routeID(r), input))
}

func (c *Controller) updateChannelTypeStatus(r *ghttp.Request) {
	var input adminapi.ChannelTypeStatusInput
	if !parse(r, &input) {
		return
	}
	c.respondAfterChannelListMutation(r, map[string]any{}, c.types.SetStatus(r.Context(), routeID(r), input.Status))
}

func (c *Controller) deleteChannelType(r *ghttp.Request) {
	c.respondAfterChannelListMutation(r, map[string]any{}, c.types.Delete(r.Context(), routeID(r)))
}

func (c *Controller) respondAfterChannelListMutation(r *ghttp.Request, data any, err error) {
	if err == nil {
		c.channels.InvalidateListCache(r.Context())
	}
	respond(r, data, err)
}
