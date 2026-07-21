package admin

import (
	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
)

func (c *Controller) registerSystemRoutes(group *ghttp.RouterGroup) {
	group.GET("/system", c.systemInfo)
	group.GET("/system/basic", c.getBaseSettings)
	group.PUT("/system/basic", c.updateBaseSettings)
	group.GET("/system/settings", c.getSystemSettings)
	group.PUT("/system/settings", c.updateSystemSettings)
	group.GET("/system/mail", c.getMailSettings)
	group.PUT("/system/mail", c.updateMailSettings)
	group.POST("/system/mail/test", c.sendMailTest)
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

func (c *Controller) getBaseSettings(r *ghttp.Request) {
	data, err := c.settings.GetBase(r.Context())
	respond(r, data, err)
}

func (c *Controller) updateBaseSettings(r *ghttp.Request) {
	var input adminapi.BaseSettingsInput
	if !parse(r, &input) {
		return
	}
	data, err := c.settings.UpdateBase(r.Context(), input)
	respond(r, data, err)
}

func (c *Controller) getSystemSettings(r *ghttp.Request) {
	data, err := c.settings.Get(r.Context())
	respond(r, data, err)
}

func (c *Controller) updateSystemSettings(r *ghttp.Request) {
	var input adminapi.SystemResilienceSettingsInput
	if !parse(r, &input) {
		return
	}
	data, err := c.settings.Update(r.Context(), input)
	respond(r, data, err)
}

func (c *Controller) getMailSettings(r *ghttp.Request) {
	data, err := c.settings.GetMailSettings(r.Context())
	respond(r, data, err)
}

func (c *Controller) updateMailSettings(r *ghttp.Request) {
	var input adminapi.MailSettingsInput
	if !parse(r, &input) {
		return
	}
	data, err := c.settings.UpdateMailSettings(r.Context(), input)
	respond(r, data, err)
}

func (c *Controller) sendMailTest(r *ghttp.Request) {
	var input adminapi.MailTestInput
	if !parse(r, &input) {
		return
	}
	respond(r, map[string]any{}, c.mail.SendTest(r.Context(), input.Recipient))
}
