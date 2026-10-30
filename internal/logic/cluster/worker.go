package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"

	"github.com/yunloli/aiferry/internal/buildinfo"
)

// sync_state 中保存的键。
const (
	stateKeyNodeID        = "node_id"
	stateKeyConfigVersion = "config_version"
)

// registerRequest 是子节点上报的注册信息。
type registerRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Address string `json:"address"`
}

// registerResponse 是主节点返回的注册结果。
type registerResponse struct {
	NodeId          uint64 `json:"nodeId"`
	ConfigVersion   int64  `json:"configVersion"`
	SyncInterval    int    `json:"syncInterval"`
	FlushInterval   int    `json:"flushInterval"`
	HeartbeatSecond int    `json:"heartbeatInterval"`
}

// heartbeatRequest 是子节点周期上报的运行时状态。
type heartbeatRequest struct {
	ConfigVersion int64  `json:"configVersion"`
	PendingRows   int64  `json:"pendingRows"`
	LastError     string `json:"lastError"`
}

// Wake 接收一次「立即同步」的唤醒信号。重复信号会被合并（通道容量为 1）。
func (s *Service) Wake() {
	if s == nil || s.wake == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// EnsureRegistered 向主节点注册并拿到节点 ID。
//
// 注册是幂等的：同名节点重复注册只刷新地址与版本，因此容器重建后不需要人工干预。
// 同时借注册返回的版本号建立本地基线。
func (s *Service) EnsureRegistered(ctx context.Context) error {
	body, err := json.Marshal(registerRequest{
		Name:    s.NodeName(),
		Version: buildinfo.RuntimeVersion(),
	})
	if err != nil {
		return gerror.Wrap(err, "encode cluster register request")
	}
	response, err := s.call(ctx, s.config.Cluster.MasterURL, "/register", body, contentTypeJSON)
	if err != nil {
		return gerror.Wrapf(err, "register node %s to %s", s.NodeName(), s.config.Cluster.MasterURL)
	}
	if response == nil {
		return gerror.New("cluster: empty register response")
	}
	reply := registerResponse{}
	if response.StatusCode != http.StatusOK {
		message, _ := readLimited(response)
		_ = response.Body.Close()
		return gerror.Newf("cluster: register rejected with %d: %s", response.StatusCode, message)
	}
	if err = decodeGzipJSON(response, &reply); err != nil {
		return err
	}
	if err = s.setLocalState(ctx, stateKeyNodeID, reply.NodeId); err != nil {
		return err
	}
	// 这里刻意不写入 reply.ConfigVersion：本地版本号必须由「真正应用到镜像的快照」
	// 产生。若在注册时就把主节点版本号当成基线，空数据目录的子节点首次同步会拿到
	// 304，镜像永远是空的。
	return nil
}

// SyncOnce 按版本号条件判断是否需要拉取配置，需要则整包应用。
//
// 子节点只在版本落后时才访问主节点，主节点正常时每分钟一次的条件请求，
// 相比「每个用户请求都回落主节点」可以忽略不计。
func (s *Service) SyncOnce(ctx context.Context) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	local, err := s.localConfigVersion(ctx)
	if err != nil {
		return err
	}
	path := "/config?version=" + formatInt(local)
	response, err := s.call(ctx, s.config.Cluster.MasterURL, path, nil, "")
	if err != nil {
		return gerror.Wrap(err, "fetch cluster config")
	}
	if response == nil {
		return gerror.New("cluster: empty config response")
	}
	if response.StatusCode == http.StatusNotModified {
		_ = response.Body.Close()
		return nil
	}
	if response.StatusCode != http.StatusOK {
		message, _ := readLimited(response)
		_ = response.Body.Close()
		return gerror.Newf("cluster: fetch config failed with %d: %s", response.StatusCode, message)
	}
	snapshot := &Snapshot{}
	if err = decodeGzipJSON(response, snapshot); err != nil {
		return err
	}
	if err = s.applyAndPersist(ctx, snapshot); err != nil {
		return err
	}
	return nil
}

// applyAndPersist 应用快照并记录版本号，然后触发本地缓存失效。
//
// 顺序很重要：先写镜像再写版本号，进程若在两者之间崩溃，重启后版本号仍是旧的，
// 会重新拉取同一份快照（幂等），不会出现「版本已更新但数据没落地」。
func (s *Service) applyAndPersist(ctx context.Context, snapshot *Snapshot) error {
	if err := applySnapshot(ctx, s.database, snapshot); err != nil {
		return gerror.Wrap(err, "apply cluster snapshot")
	}
	if err := s.setLocalState(ctx, stateKeyConfigVersion, snapshot.Version); err != nil {
		return err
	}
	if s.onConfigApplied != nil {
		if err := s.onConfigApplied(ctx); err != nil {
			s.clusterLogger().Warningf(ctx, "cluster: invalidate local caches after snapshot: %v", err)
		}
	}
	s.clusterLogger().Infof(ctx, "cluster: applied config snapshot version %d", snapshot.Version)
	return nil
}

// formatAny 把同步状态值渲染成字符串。sync_state.value 统一存文本，
// 读取时按需要转成整数，避免 SQLite 动态类型带来的比较歧义。
func formatAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return formatInt(int64(typed))
	case int64:
		return formatInt(typed)
	case uint64:
		return formatInt(int64(typed))
	default:
		return gconv.String(value)
	}
}

func formatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

// localConfigVersion 读本地已应用的配置版本，没有记录时返回 0（必然拉全量）。
func (s *Service) localConfigVersion(ctx context.Context) (int64, error) {
	value, err := s.database.GetValue(ctx,
		"SELECT value FROM sync_state WHERE state_key = ?", stateKeyConfigVersion)
	if err != nil {
		return 0, gerror.Wrap(err, "read local config version")
	}
	if value.IsNil() || value.String() == "" {
		return 0, nil
	}
	return value.Int64(), nil
}

// setLocalState 写入同步状态键值，存在则覆盖。
func (s *Service) setLocalState(ctx context.Context, key string, value any) error {
	_, err := s.database.Exec(ctx,
		"INSERT INTO sync_state(state_key, value) VALUES(?, ?) ON CONFLICT(state_key) DO UPDATE SET value = excluded.value",
		key, formatAny(value))
	if err != nil {
		return gerror.Wrapf(err, "persist sync state %s", key)
	}
	return nil
}

// pendingRows 返回本节点待回传的用量行数，用于心跳上报与管理端展示。
func (s *Service) pendingRows(ctx context.Context) int64 {
	value, err := s.database.GetValue(ctx, "SELECT COUNT(*) FROM usage_logs")
	if err != nil {
		return 0
	}
	return value.Int64()
}

// startWorkerLoops 启动子节点的常驻协程：配置同步、心跳、用量回传。
//
// 首次同步失败即返回错误：子节点没有配置就没有渠道可转发，带病启动只会让请求全部失败。
func (s *Service) startWorkerLoops(ctx context.Context) (func(), error) {
	bootstrapCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := s.EnsureRegistered(bootstrapCtx); err != nil {
		return nil, err
	}
	if err := s.SyncOnce(bootstrapCtx); err != nil {
		if s.config.Cluster.BootstrapRequired {
			return nil, gerror.Wrapf(err, "worker bootstrap requires the master at %s", s.config.Cluster.MasterURL)
		}
		s.clusterLogger().Warningf(ctx, "cluster: initial sync failed, will retry: %v", err)
	}
	if err := s.verifyMasterKey(ctx); err != nil {
		return nil, err
	}
	go s.syncLoop(ctx)
	go s.heartbeatLoop(ctx)
	go s.usageLoop(ctx)
	return func() {}, nil
}

// syncLoop 定时同步配置，并响应主节点推送。
func (s *Service) syncLoop(ctx context.Context) {
	interval := s.config.Cluster.SyncInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
			s.clusterLogger().Infof(ctx, "cluster: config change pushed by master")
		}
		if err := s.SyncOnce(ctx); err != nil {
			s.clusterLogger().Warningf(ctx, "cluster: sync config: %v", err)
			s.reportError(ctx, err)
		}
	}
}

// heartbeatLoop 周期上报版本、待回传行数与最后错误。
func (s *Service) heartbeatLoop(ctx context.Context) {
	interval := s.config.Cluster.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sendHeartbeat(ctx); err != nil {
				s.clusterLogger().Warningf(ctx, "cluster: heartbeat: %v", err)
			}
		}
	}
}

// sendHeartbeat 上报一次心跳。节点未注册时先补注册（主节点重启后注册表仍在，但
// 若被清理过则需要重新建立）。
func (s *Service) sendHeartbeat(ctx context.Context) error {
	nodeID, err := s.localInt(ctx, stateKeyNodeID)
	if err != nil {
		return err
	}
	if nodeID == 0 {
		return s.EnsureRegistered(ctx)
	}
	version, err := s.localConfigVersion(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(heartbeatRequest{
		ConfigVersion: version,
		PendingRows:   s.pendingRows(ctx),
		LastError:     "",
	})
	if err != nil {
		return gerror.Wrap(err, "encode heartbeat")
	}
	response, err := s.call(ctx, s.config.Cluster.MasterURL, "/heartbeat", body, contentTypeJSON)
	if err != nil {
		return err
	}
	if response == nil {
		return gerror.New("cluster: empty heartbeat response")
	}
	if response.StatusCode == http.StatusOK {
		_ = response.Body.Close()
		return nil
	}
	message, _ := readLimited(response)
	_ = response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		// 主节点已经删掉本节点记录（吊销或重建库），重新注册一次。
		if err = s.setLocalState(ctx, stateKeyNodeID, 0); err == nil {
			return s.EnsureRegistered(ctx)
		}
	}
	return gerror.Newf("cluster: heartbeat rejected with %d: %s", response.StatusCode, message)
}

// reportError 把同步失败摘要推给主节点，管理端节点页据此提示。
func (s *Service) reportError(ctx context.Context, cause error) {
	nodeID, err := s.localInt(ctx, stateKeyNodeID)
	if err != nil || nodeID == 0 {
		return
	}
	body, err := json.Marshal(heartbeatRequest{LastError: cause.Error()})
	if err != nil {
		return
	}
	// 尽力而为：失败不再上报错误，避免错误风暴。
	if response, callErr := s.call(ctx, s.config.Cluster.MasterURL, "/heartbeat", body, contentTypeJSON); callErr == nil && response != nil {
		_ = response.Body.Close()
	}
}

// verifyMasterKey 验证本节点主密钥与主节点一致：能解开快照里的凭据密文才算通过。
//
// 这是最容易踩的坑：密钥不一致时配置齐全但所有请求都会解密失败，
// 因此在启动阶段就快速失败，而不是等到第一个用户请求才暴露。
func (s *Service) verifyMasterKey(ctx context.Context) error {
	if s.secrets == nil {
		return nil
	}
	cipher, err := s.database.GetValue(ctx,
		"SELECT secret_encrypted FROM channel_credentials WHERE secret_encrypted <> '' LIMIT 1")
	if err != nil {
		return gerror.Wrap(err, "read credential for key verification")
	}
	if cipher.IsNil() {
		return nil
	}
	if _, err = s.secrets.Decrypt(cipher.String()); err != nil {
		return gerror.Wrapf(err,
			"worker master key does not match the master node: set AIFERRY_MASTER_KEY to the master's value")
	}
	return nil
}

func (s *Service) localInt(ctx context.Context, key string) (int64, error) {
	value, err := s.database.GetValue(ctx, "SELECT value FROM sync_state WHERE state_key = ?", key)
	if err != nil {
		return 0, gerror.Wrapf(err, "read sync state %s", key)
	}
	if value.IsNil() || value.String() == "" {
		return 0, nil
	}
	return value.Int64(), nil
}
