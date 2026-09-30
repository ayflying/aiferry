package admin

import (
	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/logic/auth"
)

func (c *Controller) registerUserRoutes(group *ghttp.RouterGroup) {
	group.GET("/users", c.listUsers)
	group.GET("/users/{id}/dashboard", c.userDashboard)
	group.PUT("/users/{id}/balance", c.updateUserBalance)
	group.DELETE("/users/{id}", c.deleteUser)
	group.GET("/users/{id}/channel-groups", c.listUserChannelGroups)
	group.POST("/users/{id}/channel-groups", c.updateUserChannelGroups)
}

func (c *Controller) listUsers(r *ghttp.Request) {
	if r.GetQuery("compact").Bool() {
		data, err := c.users.ListOptions(r.Context())
		respond(r, data, err)
		return
	}
	data, err := c.users.List(r.Context())
	respond(r, data, err)
}

func (c *Controller) userDashboard(r *ghttp.Request) {
	dateRange, err := c.usage.ParseDashboardRange(r.Context(), r.GetQuery("startAt").String(), r.GetQuery("endAt").String(), r.GetQuery("days", 30).Int(), r.GetQuery("hours").Int())
	if err != nil {
		respond(r, nil, err)
		return
	}
	data, err := c.usage.DashboardForUser(r.Context(), routeID(r), dateRange)
	respond(r, data, err)
}

func (c *Controller) updateUserBalance(r *ghttp.Request) {
	var input adminapi.UserBalanceInput
	if !parse(r, &input) {
		return
	}
	data, err := c.users.UpdateBalance(r.Context(), routeID(r), input.Balance)
	respond(r, data, err)
}

func (c *Controller) deleteUser(r *ghttp.Request) {
	operator, ok := auth.CurrentUser(r.Context())
	if !ok {
		respond(r, nil, auth.ErrUnauthorized)
		return
	}
	respond(r, map[string]any{}, c.users.Delete(r.Context(), routeID(r), operator.Id))
}

func (c *Controller) listUserChannelGroups(r *ghttp.Request) {
	data, err := c.users.ListChannelGroupIDs(r.Context(), routeID(r))
	respond(r, data, err)
}

func (c *Controller) updateUserChannelGroups(r *ghttp.Request) {
	var input adminapi.UserChannelGroupInput
	if !parse(r, &input) {
		return
	}
	data, err := c.users.ReplaceChannelGroupIDs(r.Context(), routeID(r), input.ChannelGroupIDs)
	respond(r, data, err)
}
