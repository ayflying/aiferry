package admin

import (
	"net/http"
	"strconv"

	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/service/apikey"
	"github.com/yunloli/aiferry/internal/service/channel"
	"github.com/yunloli/aiferry/internal/service/channelgroup"
	"github.com/yunloli/aiferry/internal/service/channeltype"
	"github.com/yunloli/aiferry/internal/service/usage"
)

type Controller struct {
	channels *channel.Service
	types    *channeltype.Service
	groups   *channelgroup.Service
	apiKeys  *apikey.Service
	usage    *usage.Service
}

func New(channelSvc *channel.Service, channelTypeSvc *channeltype.Service, groupSvc *channelgroup.Service, apiKeySvc *apikey.Service, usageSvc *usage.Service) *Controller {
	return &Controller{channels: channelSvc, types: channelTypeSvc, groups: groupSvc, apiKeys: apiKeySvc, usage: usageSvc}
}

func (c *Controller) Register(group *ghttp.RouterGroup) {
	group.GET("/channels", c.listChannels)
	group.POST("/channels", c.createChannel)
	group.PUT("/channels/{id}", c.updateChannel)
	group.DELETE("/channels/{id}", c.deleteChannel)
	group.GET("/channel-types", c.listChannelTypes)
	group.POST("/channel-types", c.createChannelType)
	group.PUT("/channel-types/{id}", c.updateChannelType)
	group.DELETE("/channel-types/{id}", c.deleteChannelType)
	group.GET("/channel-groups", c.listChannelGroups)
	group.POST("/channel-groups", c.createChannelGroup)
	group.PUT("/channel-groups/{id}", c.updateChannelGroup)
	group.DELETE("/channel-groups/{id}", c.deleteChannelGroup)
	group.POST("/channels/{id}/models/discover", c.discoverModels)
	group.GET("/channels/{id}/models", c.listChannelModels)
	group.PUT("/channels/{id}/models/selection", c.selectChannelModels)
	group.POST("/channels/{id}/costs/query", c.queryChannelCost)
	group.POST("/channels/{id}/prices/sync", c.syncChannelPrices)
	group.GET("/models", c.listModels)
	group.PUT("/models/{id}", c.updateModel)
	group.GET("/models/{id}/price-rules", c.listPriceRules)
	group.POST("/models/{id}/price-rules", c.createPriceRule)
	group.PUT("/price-rules/{id}", c.updatePriceRule)
	group.DELETE("/price-rules/{id}", c.deletePriceRule)
	group.POST("/models/test", c.testModel)
	group.GET("/api-keys", c.listAPIKeys)
	group.POST("/api-keys", c.createAPIKey)
	group.PUT("/api-keys/{id}", c.updateAPIKey)
	group.DELETE("/api-keys/{id}", c.deleteAPIKey)
	group.GET("/dashboard", c.dashboard)
	group.GET("/usage", c.listUsage)
	group.GET("/system", c.systemInfo)
}

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
	respond(r, map[string]any{"id": id}, err)
}
func (c *Controller) updateChannelGroup(r *ghttp.Request) {
	var input adminapi.ChannelGroupInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.groups.Update(r.Context(), routeID(r), input))
}
func (c *Controller) deleteChannelGroup(r *ghttp.Request) {
	respond(r, map[string]any{}, c.groups.Delete(r.Context(), routeID(r)))
}

func (c *Controller) listChannelTypes(r *ghttp.Request) {
	data, err := c.types.List(r.Context())
	respond(r, data, err)
}

func (c *Controller) createChannelType(r *ghttp.Request) {
	var input adminapi.ChannelTypeInput
	if !parse(r, &input) {
		return
	}
	id, err := c.types.Create(r.Context(), input)
	respond(r, map[string]any{"id": id}, err)
}

func (c *Controller) updateChannelType(r *ghttp.Request) {
	var input adminapi.ChannelTypeInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.types.Update(r.Context(), routeID(r), input))
}

func (c *Controller) deleteChannelType(r *ghttp.Request) {
	respond(r, map[string]any{}, c.types.Delete(r.Context(), routeID(r)))
}

func (c *Controller) listChannels(r *ghttp.Request) {
	data, err := c.channels.List(r.Context())
	respond(r, data, err)
}

func (c *Controller) createChannel(r *ghttp.Request) {
	var input adminapi.ChannelInput
	if !parse(r, &input) {
		return
	}
	id, err := c.channels.Create(r.Context(), input)
	respond(r, map[string]any{"id": id}, err)
}

func (c *Controller) updateChannel(r *ghttp.Request) {
	var input adminapi.ChannelInput
	if !parse(r, &input) {
		return
	}
	err := c.channels.Update(r.Context(), routeID(r), input)
	respond(r, map[string]any{}, err)
}

func (c *Controller) deleteChannel(r *ghttp.Request) {
	respond(r, map[string]any{}, c.channels.Delete(r.Context(), routeID(r)))
}

func (c *Controller) discoverModels(r *ghttp.Request) {
	data, err := c.channels.DiscoverModels(r.Context(), routeID(r))
	respond(r, data, err)
}

func (c *Controller) listChannelModels(r *ghttp.Request) {
	data, err := c.channels.ListModels(r.Context(), routeID(r))
	respond(r, data, err)
}

func (c *Controller) selectChannelModels(r *ghttp.Request) {
	var input adminapi.ModelSelectionInput
	if !parse(r, &input) {
		return
	}
	data, err := c.channels.SelectModels(r.Context(), routeID(r), input)
	respond(r, data, err)
}

func (c *Controller) listModels(r *ghttp.Request) {
	data, err := c.channels.ListModels(r.Context(), 0)
	respond(r, data, err)
}

func (c *Controller) updateModel(r *ghttp.Request) {
	var input adminapi.ModelInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.channels.UpdateModel(r.Context(), routeID(r), input))
}

func (c *Controller) testModel(r *ghttp.Request) {
	var input adminapi.ModelTestInput
	if !parse(r, &input) {
		return
	}
	data, err := c.channels.TestModel(r.Context(), input)
	respond(r, data, err)
}

func (c *Controller) queryChannelCost(r *ghttp.Request) {
	var input adminapi.CostQueryInput
	if len(r.GetBody()) > 0 && !parse(r, &input) {
		return
	}
	data, err := c.channels.QueryCost(r.Context(), routeID(r), input)
	respond(r, data, err)
}

func (c *Controller) syncChannelPrices(r *ghttp.Request) {
	count, err := c.channels.SyncPrices(r.Context(), routeID(r))
	respond(r, map[string]any{"count": count}, err)
}

func (c *Controller) listPriceRules(r *ghttp.Request) {
	data, err := c.channels.ListPriceRules(r.Context(), routeID(r))
	respond(r, data, err)
}

func (c *Controller) createPriceRule(r *ghttp.Request) {
	var input adminapi.PriceRuleInput
	if !parse(r, &input) {
		return
	}
	id, err := c.channels.CreatePriceRule(r.Context(), routeID(r), input)
	respond(r, map[string]any{"id": id}, err)
}

func (c *Controller) updatePriceRule(r *ghttp.Request) {
	var input adminapi.PriceRuleInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.channels.UpdatePriceRule(r.Context(), routeID(r), input))
}

func (c *Controller) deletePriceRule(r *ghttp.Request) {
	respond(r, map[string]any{}, c.channels.DeletePriceRule(r.Context(), routeID(r)))
}

func (c *Controller) listAPIKeys(r *ghttp.Request) {
	data, err := c.apiKeys.List(r.Context())
	respond(r, data, err)
}

func (c *Controller) createAPIKey(r *ghttp.Request) {
	var input adminapi.APIKeyInput
	if !parse(r, &input) {
		return
	}
	data, err := c.apiKeys.Create(r.Context(), input)
	respond(r, data, err)
}

func (c *Controller) updateAPIKey(r *ghttp.Request) {
	var input adminapi.APIKeyUpdate
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.apiKeys.Update(r.Context(), routeID(r), input))
}

func (c *Controller) deleteAPIKey(r *ghttp.Request) {
	respond(r, map[string]any{}, c.apiKeys.Delete(r.Context(), routeID(r)))
}

func (c *Controller) dashboard(r *ghttp.Request) {
	days := r.GetQuery("days", 7).Int()
	data, err := c.usage.Dashboard(r.Context(), days)
	respond(r, data, err)
}

func (c *Controller) listUsage(r *ghttp.Request) {
	data, err := c.usage.List(
		r.Context(),
		r.GetQuery("page", 1).Int(),
		r.GetQuery("pageSize", 20).Int(),
		r.GetQuery("model").String(),
		r.GetQuery("channelId").Uint64(),
		r.GetQuery("apiKeyId").Uint64(),
	)
	respond(r, data, err)
}

func (c *Controller) systemInfo(r *ghttp.Request) {
	respond(r, map[string]any{
		"name":       "AiFerry",
		"adminMode":  "casdoor",
		"database":   "mysql",
		"cache":      "redis",
		"apiVersion": "v1",
	}, nil)
}

func parse(r *ghttp.Request, target any) bool {
	if err := r.Parse(target); err != nil {
		r.Response.WriteStatus(http.StatusBadRequest)
		r.Response.WriteJson(map[string]any{"code": 400, "message": err.Error(), "data": nil})
		r.Exit()
		return false
	}
	return true
}

func respond(r *ghttp.Request, data any, err error) {
	if err != nil {
		r.Response.WriteStatus(http.StatusBadRequest)
		r.Response.WriteJson(map[string]any{"code": 400, "message": err.Error(), "data": nil})
		r.Exit()
		return
	}
	r.Response.WriteJson(map[string]any{"code": 0, "message": "", "data": data})
	r.Exit()
}

func routeID(r *ghttp.Request) uint64 {
	value := r.GetRouter("id").String()
	id, _ := strconv.ParseUint(value, 10, 64)
	return id
}
