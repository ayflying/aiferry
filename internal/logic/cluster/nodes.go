package cluster

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// Node 是子节点在主节点注册表中的一条记录，管理端节点页直接展示该结构。
type Node struct {
	Id            uint64    `json:"id"`
	Name          string    `json:"name"`
	Address       string    `json:"address"`
	Version       string    `json:"version"`
	ConfigVersion int64     `json:"configVersion"`
	PendingRows   int64     `json:"pendingRows"`
	LastError     string    `json:"lastError"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	LastSync      time.Time `json:"lastSync"`
	Status        int       `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	// Stale 表示该节点配置版本落后于主节点，查询时即时计算，不入库。
	Stale bool `json:"stale"`
	// Online 表示该节点当前通过 WS 长连接订阅着主节点，同样即时计算。
	Online bool `json:"online"`
	// StreamSince 是本次长连接建立时间，不在线时为空。
	StreamSince time.Time `json:"streamSince"`
	// StreamVersion 是子节点自报的运行版本，取自 WS 连接而不落库。
	StreamVersion string `json:"streamVersion"`
}

const nodeColumns = "id, name, address, version, config_version, pending_rows, last_error, " +
	"last_heartbeat_at, last_sync_at, status, created_at, updated_at"

// RegisterNode 注册或更新子节点。同名节点重复注册视为同一次加入（例如容器重建），
// 地址与版本会被刷新；已吊销的同名墓碑永久保留，禁止自助重新加入。
func (s *Service) RegisterNode(ctx context.Context, name, address, version string) (*Node, error) {
	// 唯一名称永不回收。并发吊销发生在更新前/后都不会把 status 改回 1。
	now := time.Now()
	if _, err := s.database.Exec(ctx,
		`INSERT INTO cluster_nodes(name, address, version, last_heartbeat_at)
		 SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM cluster_nodes WHERE name = ?)`,
		name, truncate(address, 255), truncate(version, 32), now, name); err != nil {
		return nil, gerror.Wrap(err, "register cluster node")
	}
	if _, err := s.database.Exec(ctx,
		`UPDATE cluster_nodes SET address = ?, version = ?, last_heartbeat_at = ?
		 WHERE name = ? AND status = 1`,
		truncate(address, 255), truncate(version, 32), now, name); err != nil {
		return nil, gerror.Wrap(err, "refresh cluster node")
	}
	return s.FindNodeByName(ctx, name)
}

// FindNodeByName 按节点名读取注册信息。
func (s *Service) FindNodeByName(ctx context.Context, name string) (*Node, error) {
	node := &Node{}
	value, err := s.database.GetValue(ctx,
		"SELECT id FROM cluster_nodes WHERE name = ? AND status = 1", name)
	if err != nil {
		return nil, gerror.Wrap(err, "find cluster node")
	}
	if value.IsNil() {
		return nil, gerror.Newf("cluster: node %q is not registered or has been revoked", name)
	}
	if err = s.database.Model("cluster_nodes").Ctx(ctx).
		Where("id = ?", value.Int64()).Scan(node); err != nil {
		return nil, gerror.Wrap(err, "load cluster node")
	}
	return node, nil
}

// FindNodeByID 按主键读取节点（含已吊销），管理端操作用。
func (s *Service) FindNodeByID(ctx context.Context, id uint64) (*Node, error) {
	node := &Node{}
	if err := s.database.Model("cluster_nodes").Ctx(ctx).Where("id = ?", id).Scan(node); err != nil {
		return nil, gerror.Wrap(err, "load cluster node")
	}
	if node.Id == 0 {
		return nil, gerror.Newf("cluster: node %d does not exist", id)
	}
	return node, nil
}

// ListNodes 返回全部节点（含已吊销），并标出配置落后与是否在线。
//
// 在线与否只看 WS 订阅是否存在，不看心跳时间：心跳只能说明「最近来过」，
// 进程僵死或网络单向中断时心跳列仍然是新鲜的，会造成误判在线。
func (s *Service) ListNodes(ctx context.Context) ([]Node, error) {
	nodes := make([]Node, 0, 8)
	if err := s.database.Model("cluster_nodes").Ctx(ctx).Order("id ASC").Scan(&nodes); err != nil {
		return nil, gerror.Wrap(err, "list cluster nodes")
	}
	version, err := s.ConfigVersion(ctx)
	if err != nil {
		return nil, err
	}
	for index := range nodes {
		nodes[index].Stale = nodes[index].Status == 1 && nodes[index].ConfigVersion < version
		if info, ok := s.hub.connected(nodes[index].Name); ok {
			nodes[index].Online = info.Online
			nodes[index].StreamSince = info.Since
			nodes[index].StreamVersion = info.Version
		}
	}
	return nodes, nil
}

// ActiveNodes 返回需要接收推送的启用节点。
func (s *Service) ActiveNodes(ctx context.Context) ([]Node, error) {
	nodes := make([]Node, 0, 8)
	if err := s.database.Model("cluster_nodes").Ctx(ctx).
		Where("status = 1").Order("id ASC").Scan(&nodes); err != nil {
		return nil, gerror.Wrap(err, "list active cluster nodes")
	}
	return nodes, nil
}

// DeleteNode 吊销节点并保留同名墓碑，避免持有旧密钥的节点自助重新注册。
func (s *Service) DeleteNode(ctx context.Context, id uint64) error {
	if _, err := s.database.Exec(ctx, "UPDATE cluster_nodes SET status = 0 WHERE id = ?", id); err != nil {
		return gerror.Wrap(err, "delete cluster node")
	}
	return nil
}

// RecordHeartbeat 更新节点的配置版本、待回传行数与最后错误。
func (s *Service) RecordHeartbeat(ctx context.Context, id uint64, configVersion, pendingRows int64, lastError string) error {
	if _, err := s.database.Exec(ctx,
		`UPDATE cluster_nodes
		 SET config_version = ?, pending_rows = ?, last_error = ?, last_heartbeat_at = NOW(3)
		 WHERE id = ?`,
		configVersion, pendingRows, truncate(lastError, 512), id); err != nil {
		return gerror.Wrap(err, "record node heartbeat")
	}
	return nil
}

// RecordSync 记录节点成功应用快照的时间与版本。
func (s *Service) RecordSync(ctx context.Context, id uint64, configVersion int64) error {
	if _, err := s.database.Exec(ctx,
		"UPDATE cluster_nodes SET config_version = ?, last_sync_at = NOW(3), last_error = '' WHERE id = ?",
		configVersion, id); err != nil {
		return gerror.Wrap(err, "record node sync")
	}
	return nil
}

// RecordNodeError 记录节点上报的错误摘要，管理端节点页直接展示。
func (s *Service) RecordNodeError(ctx context.Context, id uint64, message string) error {
	if _, err := s.database.Exec(ctx,
		"UPDATE cluster_nodes SET last_error = ? WHERE id = ?", truncate(message, 512), id); err != nil {
		return gerror.Wrap(err, "record node error")
	}
	return nil
}
