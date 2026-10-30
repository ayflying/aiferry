package cmd

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcmd"

	"github.com/yunloli/aiferry/internal/buildinfo"
	"github.com/yunloli/aiferry/internal/config"
	adminctrl "github.com/yunloli/aiferry/internal/controller/admin"
	authctrl "github.com/yunloli/aiferry/internal/controller/auth"
	clusterctrl "github.com/yunloli/aiferry/internal/controller/cluster"
	relayctrl "github.com/yunloli/aiferry/internal/controller/relay"
	"github.com/yunloli/aiferry/internal/logic/apikey"
	"github.com/yunloli/aiferry/internal/logic/app"
	"github.com/yunloli/aiferry/internal/logic/auth"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/channelgroup"
	"github.com/yunloli/aiferry/internal/logic/channeltype"
	"github.com/yunloli/aiferry/internal/logic/cluster"
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
				channelGroupSvc = channelgroup.New(appSvc.Redis)
				channelTypeSvc  = channeltype.New(builtins, appSvc.Redis)
				mailSvc         = mail.New(appSvc, systemSvc, userSvc)
				channelSvc      = channel.New(appSvc, channelTypeSvc, channelGroupSvc, systemSvc, usageSvc, priceCache, userSvc, mailSvc)
				priceSourceSvc  = pricesource.New(channelSvc)
				relaySvc        = relay.New(appSvc, usageSvc, systemSvc, userSvc, priceCache, mailSvc, channelSvc, channelTypeSvc, locationSvc)
				adminCtrl       = adminctrl.New(channelSvc, channelTypeSvc, channelGroupSvc, priceSourceSvc, apiKeySvc, systemSvc, firewallSvc, usageSvc, userSvc, authSvc, mailSvc, redemptionSvc)
				authCtrl        = authctrl.New(authSvc, userSvc, systemSvc)
				relayCtrl       = relayctrl.New(apiKeySvc, relaySvc, firewallSvc)
				s               = g.Server()
			)
			system.SetAutoDisableNotifier(systemSvc, mailSvc)
			// 主节点持有全量数据并承担价格同步、成本快照与保留期清理；
			// 子节点只维护自己的镜像与健康状态。这些后台任务在子节点上必须跳过，
			// 否则会去操作镜像里并不存在的表，或删掉尚未回传的用量明细。
			if cfg.IsMaster() {
				if err = priceCache.Load(ctx); err != nil {
					return err
				}
				if err = channelSvc.RestoreCostQueryDisabledCredentials(ctx); err != nil {
					return err
				}
				channelSvc.StartWorkBuddyCheckins(ctx)
				channelSvc.StartCostSync(ctx)
				usageSvc.StartRetentionCleanup(ctx, cfg.UsageRetentionDays)
			}
			channelSvc.StartHealthChecks(ctx)
			relay.InitPayloadLog(cfg.PayloadLogEnabled, cfg.PayloadLogDir, cfg.PayloadLogMaxFiles, cfg.PayloadLogMaxBytes)
			relay.StartPayloadCleanup(ctx)
			clusterSvc, err := cluster.New(cfg, cluster.Options{
				Store:   appSvc.Redis,
				Secrets: appSvc.Secrets,
				Prices:  priceCache,
				Client:  appSvc.HTTP,
				// 子节点回传的用量在主节点按与转发侧相同的实现扣费，
				// 否则主节点余额不会减少，下一次快照会把子节点的扣费覆盖掉。
				Billing: clusterBilling{users: userSvc, keys: apiKeySvc},
				// 子节点应用快照后必须让路由缓存与价格缓存失效，
				// 否则新配置要等到缓存过期才生效。
				OnConfigApplied: func(ctx context.Context) error {
					if loadErr := priceCache.Load(ctx); loadErr != nil {
						return loadErr
					}
					return relaySvc.InvalidateRouteCache(ctx)
				},
			})
			if err != nil {
				return err
			}
			clusterCtrl := clusterctrl.New(clusterSvc)
			if _, err = clusterSvc.Start(ctx); err != nil {
				return err
			}
			s.SetAddr(":8080")
			if cfg.IsMaster() {
				s.SetServerRoot(cfg.WebRoot)
				s.SetFileServerEnabled(true)
				s.SetIndexFolder(false)
				s.SetIndexFiles([]string{"index.html"})
			} else {
				// 子节点不暴露管理端：没有静态文件，也没有 SPA 回落。
				s.SetFileServerEnabled(false)
			}
			// 前端入口（/ 与 /channels 等 SPA 路径）响应必须 no-cache：
			// GoFrame 静态文件优先级高于动态路由，根路径由内置服务直接命中 index.html，
			// BindHandler 注册的路由不会执行，因此用 BeforeServe Hook 统一加头。
			// 否则发版后浏览器按启发式缓存沿用旧 index.html，继续引用旧 hash 的 JS 分包。
			s.BindHookHandler("/*", ghttp.HookBeforeServe, func(r *ghttp.Request) {
				if r.Method == http.MethodGet || r.Method == http.MethodHead {
					r.Response.Header().Set("Cache-Control", "no-cache")
				}
			})
			s.BindHandler("GET:/healthz", func(r *ghttp.Request) {
				r.Response.WriteJson(map[string]any{
					"status":   "ok",
					"version":  buildinfo.RuntimeVersion(),
					"revision": buildinfo.Revision,
				})
			})
			// 未配置集群密钥时完全不注册内部接口，保持与单机部署完全一致。
			if clusterSvc.Enabled() {
				s.Group("/internal/cluster", func(group *ghttp.RouterGroup) {
					// 主节点提供注册/快照/心跳，子节点只接收配置变更通知。
					if cfg.IsMaster() {
						clusterCtrl.RegisterForMaster(group)
						return
					}
					clusterCtrl.RegisterForWorker(group)
				})
			}
			if cfg.IsMaster() {
				s.Group("/api/auth", func(group *ghttp.RouterGroup) {
					authCtrl.RegisterPublic(group)
				})
				s.Group("/api/auth", func(group *ghttp.RouterGroup) {
					authCtrl.RegisterProtected(group)
				})
				s.BindHandler("GET:/auth/casdoor/callback", authCtrl.Callback)
				s.Group("/api/admin", func(group *ghttp.RouterGroup) {
					group.Middleware(authSvc.RequireUser)
					clusterCtrl.RegisterAdmin(group)
					adminCtrl.Register(group)
				})
			}
			s.Group("/v1", func(group *ghttp.RouterGroup) {
				relayCtrl.Register(group)
			})
			if cfg.IsMaster() {
				s.BindHandler("GET:/*path", func(r *ghttp.Request) {
					// index.html 必须每次回源校验，否则发版后手机 WebView 会因启发式缓存
					// 继续引用旧 hash 的 JS 分包，出现"已修复但客户端仍是旧版"的问题。
					r.Response.Header().Set("Cache-Control", "no-cache")
					path := filepath.Join(cfg.WebRoot, filepath.Clean("/"+r.GetRouter("path").String()))
					if filepath.IsAbs(path) && filepath.Clean(path) != filepath.Clean(cfg.WebRoot) {
						r.Response.ServeFile(path)
						if r.Response.Status > 0 && r.Response.Status != http.StatusNotFound {
							return
						}
					}
					r.Response.ServeFile(filepath.Join(cfg.WebRoot, "index.html"))
				})
			}
			s.Run()
			return nil
		},
	}
)
