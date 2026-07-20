package admin

import (
	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/service/auth"
	"github.com/yunloli/aiferry/internal/service/redemption"
)

func (c *Controller) registerRedemptionCodeRoutes(group *ghttp.RouterGroup) {
	group.GET("/redemption-codes", c.listRedemptionCodes)
	group.POST("/redemption-codes", c.createRedemptionCodes)
	group.DELETE("/redemption-codes/invalid", c.deleteInvalidRedemptionCodes)
}

func (c *Controller) listRedemptionCodes(r *ghttp.Request) {
	redemptionNoStore(r)
	data, err := c.redemptions.List(r.Context(), redemption.ListFilter{
		Keyword: r.GetQuery("keyword").String(),
		Status:  r.GetQuery("status").String(),
	})
	respond(r, data, err)
}

func (c *Controller) createRedemptionCodes(r *ghttp.Request) {
	redemptionNoStore(r)
	var input adminapi.RedemptionCodeCreateInput
	if !parse(r, &input) {
		return
	}
	current, ok := auth.CurrentUser(r.Context())
	if !ok {
		respond(r, nil, auth.ErrUnauthorized)
		return
	}
	data, err := c.redemptions.Create(r.Context(), current.Id, input)
	respond(r, data, err)
}

func (c *Controller) deleteInvalidRedemptionCodes(r *ghttp.Request) {
	deleted, err := c.redemptions.DeleteInvalid(r.Context())
	respond(r, map[string]int{"deleted": deleted}, err)
}

func (c *Controller) redeemCode(r *ghttp.Request) {
	redemptionNoStore(r)
	var input adminapi.RedemptionCodeRedeemInput
	if !parse(r, &input) {
		return
	}
	current, ok := auth.CurrentUser(r.Context())
	if !ok {
		respond(r, nil, auth.ErrUnauthorized)
		return
	}
	data, err := c.redemptions.Redeem(r.Context(), current.Id, input)
	respond(r, data, err)
}

func redemptionNoStore(r *ghttp.Request) {
	r.Response.Header().Set("Cache-Control", "no-store")
}
