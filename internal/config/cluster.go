package config

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// Role 区分集群中的节点职责：主节点持有数据库、缓存、管理端与后台任务；
// 子节点只承接 /v1 数据面，配置来自主节点下发的快照，本地以内置 SQLite 落地。
type Role string

const (
	// RoleMaster 是历史单机形态，也是缺省值：未设置任何集群变量时行为与旧版本完全一致。
	RoleMaster Role = "master"
	// RoleWorker 是纯数据面节点：不连 MySQL/Redis/Casdoor，只依赖主节点与内置 SQLite。
	RoleWorker Role = "worker"
)

// Cluster 汇总角色相关的运行参数。零值（Role 为空）视为 master 单机模式。
type Cluster struct {
	Role Role
	// NodeName 是节点在管理端展示的名字，worker 缺省取主机名。
	NodeName string
	// MasterURL 是 worker 访问主节点集群接口的基地址，例如 http://aiferry-master:8080。
	MasterURL string
	// Secret 是节点间 HMAC 签名密钥。主节点未配置时集群能力关闭（单机兼容），
	// worker 必须配置。
	Secret string
	// SyncInterval 是 worker 兜底轮询间隔：只做带版本号的条件请求，未变化时主节点
	// 直接返回 304，不会整包下发。
	SyncInterval time.Duration
	// FlushInterval 是 worker 批量回传用量与扣费的间隔。
	FlushInterval time.Duration
	// HeartbeatInterval 是 worker 上报心跳（配置版本、待回传条数、最后错误）的间隔。
	HeartbeatInterval time.Duration
	// FingerprintInterval 是主节点巡检配置表指纹的间隔，用于兜住「漏调用版本递增」
	// 或直接改库造成的静默不同步。
	FingerprintInterval time.Duration
	// StaleAfter 判定节点配置过期的阈值，超过后管理端标红并记 WARN 日志。
	StaleAfter time.Duration
	// PushTimeout 是主节点向单个 worker 推送变更的超时时间。
	PushTimeout time.Duration
	// BatchSize 是单次回传的最大行数。
	BatchSize int
	// OutboxMaxRows 是 worker 待回传队列的行数上限，超限丢最旧行并计数告警。
	OutboxMaxRows int
	// BootstrapRequired 为 true 时，worker 启动必须先同步一次配置成功才开监听，
	// 避免空配置对外放行免费流量。
	BootstrapRequired bool
	// DataDir 是 worker 内置 SQLite 与本地状态的落盘目录。
	DataDir string
}

// Enabled 表示节点间通信是否配置完成。主节点未配置 Secret 时为空闲状态，
// 即退化为原单机形态，不注册任何内部集群接口。
func (c Cluster) Enabled() bool {
	return strings.TrimSpace(c.Secret) != ""
}

// LoadCluster 读取集群相关环境变量。校验按角色区分：master 保持旧的三项强校验，
// worker 只校验自身必需的几项。
func LoadCluster() (Cluster, Role, error) {
	role, err := parseRole(os.Getenv("AIFERRY_ROLE"))
	if err != nil {
		return Cluster{}, "", err
	}
	cluster := Cluster{
		Role:                role,
		NodeName:            strings.TrimSpace(os.Getenv("AIFERRY_NODE_NAME")),
		MasterURL:           strings.TrimRight(env("AIFERRY_MASTER_URL", ""), "/"),
		Secret:              strings.TrimSpace(os.Getenv("AIFERRY_CLUSTER_SECRET")),
		SyncInterval:        envSeconds("AIFERRY_SYNC_INTERVAL", 60),
		FlushInterval:       envSeconds("AIFERRY_FLUSH_INTERVAL", 30),
		HeartbeatInterval:   envSeconds("AIFERRY_HEARTBEAT_INTERVAL", 30),
		FingerprintInterval: envSeconds("AIFERRY_FINGERPRINT_INTERVAL", 120),
		StaleAfter:          envSeconds("AIFERRY_STALE_AFTER", 600),
		PushTimeout:         envSeconds("AIFERRY_PUSH_TIMEOUT", 3),
		BatchSize:           envInt("AIFERRY_FLUSH_BATCH_SIZE", 500),
		OutboxMaxRows:       envInt("AIFERRY_OUTBOX_MAX_ROWS", 50000),
		BootstrapRequired:   envBool("AIFERRY_BOOTSTRAP_REQUIRED", true),
		DataDir:             env("AIFERRY_DATA_DIR", "/app/data"),
	}
	if role == RoleWorker {
		if cluster.NodeName == "" {
			cluster.NodeName, _ = os.Hostname()
		}
		if cluster.NodeName == "" {
			return Cluster{}, role, gerror.New("AIFERRY_NODE_NAME is required for worker (hostname unavailable)")
		}
		if err := validateMasterURL(cluster.MasterURL); err != nil {
			return Cluster{}, role, err
		}
		if len(cluster.Secret) < 32 {
			return Cluster{}, role, gerror.New("AIFERRY_CLUSTER_SECRET must contain at least 32 characters")
		}
	}
	if role == RoleMaster && cluster.NodeName == "" {
		cluster.NodeName = "master"
	}
	return cluster, role, nil
}

func parseRole(raw string) (Role, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", string(RoleMaster):
		return RoleMaster, nil
	case string(RoleWorker):
		return RoleWorker, nil
	default:
		return "", gerror.Newf("AIFERRY_ROLE must be %q or %q, got %q", RoleMaster, RoleWorker, raw)
	}
}

func validateMasterURL(raw string) error {
	if raw == "" {
		return gerror.New("AIFERRY_MASTER_URL is required for worker")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return gerror.Wrap(err, "AIFERRY_MASTER_URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return gerror.New("AIFERRY_MASTER_URL must use http or https")
	}
	if parsed.Host == "" {
		return gerror.New("AIFERRY_MASTER_URL must include host")
	}
	return nil
}

func envSeconds(key string, fallbackSeconds int) time.Duration {
	return time.Duration(envInt(key, fallbackSeconds)) * time.Second
}
