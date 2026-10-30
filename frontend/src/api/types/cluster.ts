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
  createdAt: string
  updatedAt: string
}

export interface ClusterNodeList {
  nodes: ClusterNode[]
}
