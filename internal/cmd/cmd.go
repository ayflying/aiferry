package cmd

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcmd"

	"github.com/yunloli/aiferry/internal/config"
	adminctrl "github.com/yunloli/aiferry/internal/controller/admin"
	authctrl "github.com/yunloli/aiferry/internal/controller/auth"
	relayctrl "github.com/yunloli/aiferry/internal/controller/relay"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channelgroup"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/iplocation"
	"github.com/yunloli/aiferry/internal/logic/mail"
	"github.com/yunloli/aiferry/internal/logic/pricesource"
	"github.com/yunloli/aiferry/internal/logic/pricingcache"
	"github.com/yunloli/aiferry/internal/logic/redemption"
	"github.com/yunloli/aiferry/internal/logic/relay"
	"github.com/yunloli/aiferry/internal/logic/requestfirewall"
	"github.com/yunloli/aiferry/internal/logic/system"
	"github.com/yunloli/aiferry/internal/logic/usage"
	"github.com/yunloli/aiferry/internal/logic/user"
)

var (
	Main = gcmd.Command{
		Name:  "main",
		Usage: "main",
		Brief: "start AiFerry server",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			builtins, err := config.LoadBuiltins(filepath.Join(filepath.Dir(cfg.MigrationsDir), "builtins.json"))
			if err != nil {
				return err
			}
			if err = channeltype.ValidateBuiltins(builtins); err != nil {
				return err
			}
			appSvc, err := app.New(ctx, cfg)
			if err != nil {
				return err
			}
			locationSvc := iplocation.New(ctx, appSvc.HTTP, filepath.Join(filepath.Dir(cfg.WebRoot), "data"))
			var (
				systemSvc       = system.New(appSvc)
				apiKeySvc       = apikey.New(appSvc)
				authSvc         = auth.New(appSvc, systemSvc)
				usageSvc        = usage.New(locationSvc, systemSvc)
				userSvc         = user.New(appSvc, usageSvc)
				redemptionSvc   = redemption.New(userSvc)
				firewallSvc     = requestfirewall.New(systemSvc)
				priceCache      = pricingcache.New()
				channelGroupSvc = channelgroup.New()
				channelTypeSvc  = channeltype.New(builtins)
				mailSvc         = mail.New(appSvc, systemSvc, userSvc)
				channelSvc      = channel.New(appSvc, channelTypeSvc, channelGroupSvc, systemSvc, usageSvc, priceCache, userSvc, mailSvc)
				priceSourceSvc  = pricesource.New(channelSvc)
				relaySvc        = relay.New(appSvc, usageSvc, systemSvc, userSvc, priceCache, mailSvc, channelSvc, locationSvc)
				adminCtrl       = adminctrl.New(channelSvc, channelTypeSvc, channelGroupSvc, priceSourceSvc, apiKeySvc, systemSvc, firewallSvc, usageSvc, userSvc, authSvc, mailSvc, redemptionSvc)
				authCtrl        = authctrl.New(authSvc, userSvc, systemSvc)
				relayCtrl       = relayctrl.New(apiKeySvc, relaySvc, firewallSvc)
				s               = g.Server()
			)
			if err = priceCache.Load(ctx); err != nil {
				return err
			}
			if err = channelSvc.RestoreCostQueryDisabledCredentials(ctx); err != nil {
				return err
			}
			channelSvc.StartHealthChecks(ctx)
			channelSvc.StartCostSync(ctx)
			s.SetAddr(":8080")
			s.SetServerRoot(cfg.WebRoot)
			s.SetFileServerEnabled(true)
			s.SetIndexFolder(false)
			s.SetIndexFiles([]string{"index.html"})
			s.BindHandler("GET:/healthz", func(r *ghttp.Request) {
				r.Response.WriteJson(map[string]any{"status": "ok"})
			})
			s.Group("/api/auth", func(group *ghttp.RouterGroup) {
				authCtrl.RegisterPublic(group)
			})
			s.Group("/api/auth", func(group *ghttp.RouterGroup) {
				authCtrl.RegisterProtected(group)
			})
			s.BindHandler("GET:/auth/casdoor/callback", authCtrl.Callback)
			s.Group("/api/admin", func(group *ghttp.RouterGroup) {
				group.Middleware(authSvc.RequireUser)
				adminCtrl.Register(group)
			})
			s.Group("/v1", func(group *ghttp.RouterGroup) {
				relayCtrl.Register(group)
			})
			s.BindHandler("GET:/*path", func(r *ghttp.Request) {
				path := filepath.Join(cfg.WebRoot, filepath.Clean("/"+r.GetRouter("path").String()))
				if filepath.IsAbs(path) && filepath.Clean(path) != filepath.Clean(cfg.WebRoot) {
					r.Response.ServeFile(path)
					if r.Response.Status > 0 && r.Response.Status != http.StatusNotFound {
						return
					}
				}
				r.Response.ServeFile(filepath.Join(cfg.WebRoot, "index.html"))
			})
			s.Run()
			return nil
		},
	}
)
