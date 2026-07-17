package admin

import (
	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/service/channel"
)

func (c *Controller) registerPriceRoutes(group *ghttp.RouterGroup) {
	group.GET("/price-sources", c.listPriceSources)
	group.POST("/price-sources", c.createPriceSource)
	group.PUT("/price-sources/{id}", c.updatePriceSource)
	group.DELETE("/price-sources/{id}", c.deletePriceSource)
	group.POST("/channels/{id}/prices/sync", c.syncChannelPrices)
	group.POST("/prices/sync", c.syncPrices)
}

func (c *Controller) listPriceSources(r *ghttp.Request) {
	data, err := c.prices.List(r.Context())
	respond(r, data, err)
}

func (c *Controller) createPriceSource(r *ghttp.Request) {
	var input adminapi.PriceSourceInput
	if !parse(r, &input) {
		return
	}
	id, err := c.prices.Create(r.Context(), input)
	respond(r, map[string]any{"id": id}, err)
}

func (c *Controller) updatePriceSource(r *ghttp.Request) {
	var input adminapi.PriceSourceInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.prices.Update(r.Context(), routeID(r), input))
}

func (c *Controller) deletePriceSource(r *ghttp.Request) {
	respond(r, map[string]any{}, c.prices.Delete(r.Context(), routeID(r)))
}

func (c *Controller) syncChannelPrices(r *ghttp.Request) {
	data, err := c.channels.SyncPriceSource(r.Context(), routeID(r))
	respond(r, data, err)
}

func (c *Controller) syncPrices(r *ghttp.Request) {
	var input adminapi.PriceSyncInput
	if len(r.GetBody()) > 0 && !parse(r, &input) {
		return
	}
	var (
		data channel.PriceSyncResult
		err  error
	)
	switch {
	case input.PriceSourceID > 0:
		data, err = c.prices.Sync(r.Context(), input.PriceSourceID)
	case input.ChannelID > 0:
		data, err = c.channels.SyncPriceSource(r.Context(), input.ChannelID)
	default:
		data, err = c.channels.SyncAllPrices(r.Context())
	}
	respond(r, data, err)
}
