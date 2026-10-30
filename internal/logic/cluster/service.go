package cluster

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/shopspring/decimal"

	"github.com/yunloli/aiferry/internal/config"
	"github.com/yunloli/aiferry/internal/logic/cache"
	"github.com/yunloli/aiferry/internal/logic/secret"
)

// Logger 是集群服务需要的日志能力，用接口隔离以便测试替换。
type Logger interface {
	Warningf(ctx context.Context, format string, args ...any)
	Infof(ctx context.Context, format string, args ...any)
}

// PriceReloader 抽象价格缓存的重载入口。子节点应用完快照后必须重新加载价格，
// 否则新上架的模型在本节点会被当成「无价格」而免费转发。
type PriceReloader interface {
	Load(ctx context.Context) error
}

// Billing 抽象扣费与密钥消耗累计。
//
// 集群层不直接依赖 user / apikey 逻辑：这两个包已经依赖集群（写路径要通知推送），
// 反向依赖会形成循环。装配方注入同一套实现，保证与转发侧记账语义一致。
type Billing interface {
	Debit(ctx context.Context, userID uint64, amount decimal.Decimal) error
	AddSpend(ctx context.Context, apiKeyID uint64, amount float64) error
}

// Options 是装配集群服务需要的依赖，集中成一个结构体避免构造函数参数膨胀。
type Options struct {
	Store   cache.Cache
	Secrets *secret.Service
	Prices  PriceReloader
	Client  *http.Client
	Logger  Logger
	// Billing 用于主节点对子节点回传的用量扣费；未注入时只入账不扣费。
	Billing Billing
	// OnConfigApplied 在子节点成功应用快照后调用，用于让路由缓存与价格缓存失效。
	// 用回调而不是直接依赖 relay：集群层不应反向依赖转发层，否则会形成循环引用。
	OnConfigApplied func(ctx context.Context) error
}

// Service 是集群能力的唯一入口：主节点负责版本号、快照与批量记账，
// 子节点负责拉取配置、落地镜像与回传用量。两侧共用同一份签名与协议实现。
type Service struct {
	config   config.App
	store    cache.Cache
	secrets  *secret.Service
	prices   PriceReloader
	logger   Logger
	signer   *Signer
	client   *http.Client
	database gdb.DB
	// billing 用于主节点对回传用量扣费。
	billing Billing
	// onConfigApplied 在子节点应用快照后触发缓存失效。
	onConfigApplied func(ctx context.Context) error
	// wake 用于把「主节点推送」转换成一次同步，容量 1 即天然合并重复通知。
	wake chan struct{}

	// 主节点侧：配置表指纹，用于兜底发现漏掉的版本递增。
	fingerprintMu    sync.Mutex
	fingerprintValue string
	// 主节点侧：推送去抖，短时间内的多次变更只推最终版本一次。
	pushMu      sync.Mutex
	pushPending bool
	// 子节点侧：同一时刻只允许一次配置同步，避免推送与轮询并发写镜像。
	syncMu sync.Mutex
}

// New 构造集群服务。store 是节点内缓存（主节点 Redis、子节点 SQLite），
// 同时用于签名 nonce 去重。
func New(cfg config.App, options Options) (*Service, error) {
	database, err := gdb.Instance()
	if err != nil {
		return nil, gerror.Wrap(err, "resolve database for cluster service")
	}
	service := &Service{
		config:          cfg,
		store:           options.Store,
		secrets:         options.Secrets,
		prices:          options.Prices,
		logger:          options.Logger,
		signer:          NewSigner(cfg.Cluster.Secret, options.Store),
		client:          options.Client,
		database:        database,
		billing:         options.Billing,
		onConfigApplied: options.OnConfigApplied,
		wake:            make(chan struct{}, 1),
	}
	if service.client == nil {
		service.client = &http.Client{Timeout: 30 * time.Second}
	}
	return service, nil
}

// Enabled 表示当前进程承担集群职责。主节点未配置 AIFERRY_CLUSTER_SECRET 时为 false，
// 此时完全退化为单机形态，不注册任何内部接口。
func (s *Service) Enabled() bool {
	if s == nil {
		return false
	}
	return s.config.Cluster.Enabled()
}

// IsWorker 判断当前节点角色。
func (s *Service) IsWorker() bool {
	return s.config.IsWorker()
}

// NodeName 返回本节点名，用于签名头与心跳。
func (s *Service) NodeName() string {
	return s.config.Cluster.NodeName
}

// Start 启动角色对应的后台协程。
func (s *Service) Start(ctx context.Context) (func(), error) {
	if !s.Enabled() {
		return func() {}, nil
	}
	activeService.Store(s)
	var (
		stop func()
		err  error
	)
	if s.IsWorker() {
		stop, err = s.startWorkerLoops(ctx)
	} else {
		stop, err = s.startMasterLoops(ctx)
	}
	if err != nil {
		activeService.CompareAndSwap(s, nil)
		return func() {}, err
	}
	return func() {
		activeService.CompareAndSwap(s, nil)
		if stop != nil {
			stop()
		}
	}, nil
}

// startMasterLoops 只跑「发现配置漂移」的巡检；推送本身是事件驱动的，不需要常驻协程。
func (s *Service) startMasterLoops(ctx context.Context) (func(), error) {
	interval := s.config.Cluster.FingerprintInterval
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	go func() {
		// 启动时先建立基线，避免把「刚启动」误判成「配置漂移」。
		if _, err := s.WatchFingerprint(ctx); err != nil && s.logger != nil {
			s.logger.Warningf(ctx, "cluster fingerprint baseline: %v", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				changed, err := s.WatchFingerprint(ctx)
				if err != nil && s.logger != nil {
					s.logger.Warningf(ctx, "cluster fingerprint scan: %v", err)
					continue
				}
				if changed && s.logger != nil {
					s.logger.Infof(ctx, "cluster config drift detected, version bumped")
				}
			}
		}
	}()
	return func() {}, nil
}
