// 子节点在集群中的状态。字段名与 internal/logic/cluster.Node 的 JSON 标签保持一致。
export interface ClusterNode {
  id: number
  name: string
  address: string
  version: string
  configVersion: number
  pendingRows: number
  lastError: string
  lastHeartbeat: string
  lastSync: string
  status: number
  stale: boolean
  // online 表示该节点当前通过 WS 长连接订阅着主节点，由主节点内存实时计算。
  online: boolean
  // streamSince 是本次长连接建立时间，不在线时为空字符串。
  streamSince: string
  // streamVersion 是子节点在握手中自报的运行版本，未连接时为空。
  streamVersion: string
  createdAt: string
  updatedAt: string
}

export interface ClusterNodeList {
  nodes: ClusterNode[]
}
