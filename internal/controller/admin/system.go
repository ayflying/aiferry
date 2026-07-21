package admin

import (
	"github.com/gogf/gf/v2/net/ghttp"

	adminapi "github.com/yunloli/aiferry/api/admin"
	"github.com/yunloli/aiferry/internal/service/system"
)

func (c *Controller) registerSystemRoutes(group *ghttp.RouterGroup) {
	group.GET("/system", c.systemInfo)
	group.GET("/system/basic", c.getBaseSettings)
	group.PUT("/system/basic", c.updateBaseSettings)
	group.GET("/system/information", c.getSystemInformation)
	group.PUT("/system/information", c.updateSystemInformation)
	group.GET("/system/settings", c.getSystemSettings)
	group.PUT("/system/settings", c.updateSystemSettings)
	group.GET("/system/sensitive-words", c.getSensitiveWordSettings)
	group.PUT("/system/sensitive-words", c.updateSensitiveWordSettings)
	group.GET("/system/mail", c.getMailSettings)
	group.PUT("/system/mail", c.updateMailSettings)
	group.POST("/system/mail/test", c.sendMailTest)
}

func (c *Controller) systemInfo(r *ghttp.Request) {
	name := system.DefaultSystemInformation().SystemName
	if information, err := c.settings.GetSystemInformation(r.Context()); err == nil {
		name = information.SystemName
	}
	respond(r, map[string]any{
		"name":       name,
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

func (c *Controller) getSystemInformation(r *ghttp.Request) {
	data, err := c.settings.GetSystemInformation(r.Context())
	respond(r, data, err)
}

func (c *Controller) updateSystemInformation(r *ghttp.Request) {
	var input adminapi.SystemInformationInput
	if !parse(r, &input) {
		return
	}
	data, err := c.settings.UpdateSystemInformation(r.Context(), input)
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

func (c *Controller) getSensitiveWordSettings(r *ghttp.Request) {
	data, err := c.settings.GetSensitiveWordSettings(r.Context())
	respond(r, data, err)
}

func (c *Controller) updateSensitiveWordSettings(r *ghttp.Request) {
	var input adminapi.SensitiveWordSettingsInput
	if !parse(r, &input) {
		return
	}
	data, err := c.settings.UpdateSensitiveWordSettings(r.Context(), input)
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
