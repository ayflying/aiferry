package cluster

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net"
	"net/http"

	"github.com/gogf/gf/v2/errors/gerror"
)

// workerListenPort 是容器内固定监听端口，宿主机映射端口不参与集群内部通信。
const workerListenPort = "8080"

// VerifiedNodeName 校验签名并返回自称的节点名，不要求节点已注册。
// 注册接口必须用它：调用方此时还没有节点记录。
func (s *Service) VerifiedNodeName(ctx context.Context, r *http.Request, body []byte) (string, error) {
	if !s.Enabled() {
		return "", gerror.New("cluster: internal endpoints are disabled on this node")
	}
	return s.signer.Verify(ctx, r, body)
}

// Authorize 校验签名并要求节点已注册、未被吊销。
func (s *Service) Authorize(ctx context.Context, r *http.Request, body []byte) (*Node, error) {
	name, err := s.VerifiedNodeName(ctx, r, body)
	if err != nil {
		return nil, err
	}
	return s.FindNodeByName(ctx, name)
}

// RegisterInput 是注册接口除签名外的输入。
type RegisterInput struct {
	Name       string
	Address    string
	Version    string
	RemoteAddr string
}

// HandleRegister 处理子节点注册。地址优先用子节点自报，缺失时按「来源 IP + 监听端口」推断，
// 推断值只用于推送，推不到也不影响正确性（子节点仍会条件轮询）。
func (s *Service) HandleRegister(ctx context.Context, input RegisterInput) (*registerResponse, error) {
	if input.Address == "" {
		input.Address = guessAddress(input.RemoteAddr)
	}
	address, err := clusterNodeAddress(input.Address)
	if err != nil {
		return nil, err
	}
	node, err := s.RegisterNode(ctx, input.Name, address, input.Version)
	if err != nil {
		return nil, err
	}
	version, err := s.ConfigVersion(ctx)
	if err != nil {
		return nil, err
	}
	s.clusterLogger().Infof(ctx, "cluster: node %s registered from %s", node.Name, address)
	return &registerResponse{
		NodeId:          node.Id,
		ConfigVersion:   version,
		SyncInterval:    int(s.config.Cluster.SyncInterval.Seconds()),
		FlushInterval:   int(s.config.Cluster.FlushInterval.Seconds()),
		HeartbeatSecond: int(s.config.Cluster.HeartbeatInterval.Seconds()),
	}, nil
}

// ParseQueryInt 解析内部接口的整数查询参数，空值按 0 处理。
func ParseQueryInt(raw string) (int64, error) {
	return parseInt64(raw)
}

// ConfigPayload 返回给子节点的快照（gzip 压缩）。版本未变化时返回 notModified=true，
// 让子节点用一条空响应结束同步，避免无谓的整包传输。
func (s *Service) ConfigPayload(ctx context.Context, since int64) (payload []byte, notModified bool, err error) {
	version, err := s.ConfigVersion(ctx)
	if err != nil {
		return nil, false, err
	}
	if since > 0 && since >= version {
		return nil, true, nil
	}
	snapshot, err := buildSnapshot(ctx, version)
	if err != nil {
		return nil, false, err
	}
	payload, err = encodeGzipJSON(snapshot)
	if err != nil {
		return nil, false, err
	}
	return payload, false, nil
}

// HandleHeartbeat 记录节点上报的版本、积压行数与错误摘要。
func (s *Service) HandleHeartbeat(ctx context.Context, node *Node, body []byte) error {
	request := heartbeatRequest{}
	if err := json.Unmarshal(body, &request); err != nil {
		return gerror.Wrap(err, "decode heartbeat")
	}
	return s.RecordHeartbeat(ctx, node.Id, request.ConfigVersion, request.PendingRows, request.LastError)
}

// NotifyConfigVersion 处理主节点的推送：只在主节点版本更新时唤醒一次同步。
func (s *Service) NotifyConfigVersion(ctx context.Context, version int64) error {
	local, err := s.localConfigVersion(ctx)
	if err != nil {
		return err
	}
	if version > 0 && version <= local {
		return nil
	}
	s.Wake()
	return nil
}

// EncodeGzipJSON 把响应体编码成 gzip JSON，供内部接口复用。
func EncodeGzipJSON(value any) ([]byte, error) {
	return encodeGzipJSON(value)
}

// encodeGzipJSON 序列化并压缩快照。
func encodeGzipJSON(value any) ([]byte, error) {
	buffer := &bytes.Buffer{}
	writer := gzip.NewWriter(buffer)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		_ = writer.Close()
		return nil, gerror.Wrap(err, "encode cluster snapshot")
	}
	if err := writer.Close(); err != nil {
		return nil, gerror.Wrap(err, "compress cluster snapshot")
	}
	return buffer.Bytes(), nil
}

// guessAddress 由来源地址推断子节点回调地址：容器内固定监听 8080。
func guessAddress(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || host == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, workerListenPort)
}
