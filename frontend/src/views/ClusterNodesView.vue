<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RefreshCw, Server, Trash2 } from '@lucide/vue'
import { ElMessageBox } from 'element-plus'
import { apiDelete, apiGet } from '../api/client'
import type { ClusterNode, ClusterNodeList } from '../api/types'
import TableActionButton from '../components/TableActionButton.vue'
import { showError, showSuccess } from '../lib/error'
import { formatTime } from '../lib/format'

const loading = ref(false)
const nodes = ref<ClusterNode[]>([])

async function load() {
  loading.value = true
  try {
    const result = await apiGet<ClusterNodeList>('/cluster/nodes')
    nodes.value = result.nodes ?? []
  } catch (error) {
    showError(error, '加载节点列表失败')
  } finally {
    loading.value = false
  }
}

async function revoke(node: ClusterNode) {
  try {
    await ElMessageBox.confirm(
      `吊销后节点 ${node.name} 将无法再注册，需要重新下发集群密钥才能加入。`,
      '吊销节点',
      { type: 'warning', confirmButtonText: '吊销', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await apiDelete<{ ok: boolean }>(`/cluster/nodes/${node.id}`)
    showSuccess('节点已吊销')
    await load()
  } catch (error) {
    showError(error, '吊销节点失败')
  }
}

function statusText(node: ClusterNode) {
  if (node.stale) return '失联'
  return node.status === 1 ? '在线' : '停用'
}

function statusTag(node: ClusterNode) {
  if (node.stale) return 'danger'
  return node.status === 1 ? 'success' : 'info'
}

onMounted(load)
</script>

<template>
  <section class="cluster-nodes">
    <header class="page-head">
      <div>
        <h1>集群节点</h1>
        <p class="hint">
          子节点只跑转发，配置由主节点推送；这里显示它们的配置版本、待回传用量与最近一次错误。
        </p>
      </div>
      <el-button :loading="loading" @click="load">
        <RefreshCw :size="16" />
        <span>刷新</span>
      </el-button>
    </header>

    <el-table v-loading="loading" :data="nodes" border stripe empty-text="还没有子节点注册">
      <el-table-column prop="name" label="节点" min-width="140">
        <template #default="{ row }">
          <div class="node-name">
            <Server :size="15" />
            <span>{{ row.name }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="address" label="地址" min-width="180" show-overflow-tooltip />
      <el-table-column prop="version" label="版本" width="110" />
      <el-table-column prop="configVersion" label="配置版本" width="110" />
      <el-table-column label="待回传" width="110">
        <template #default="{ row }">{{ row.pendingRows }}</template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTag(row)" disable-transitions>{{ statusText(row) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="最近心跳" width="180">
        <template #default="{ row }">{{ row.lastHeartbeat ? formatTime(row.lastHeartbeat) : '—' }}</template>
      </el-table-column>
      <el-table-column label="最近同步" width="180">
        <template #default="{ row }">{{ row.lastSync ? formatTime(row.lastSync) : '—' }}</template>
      </el-table-column>
      <el-table-column prop="lastError" label="最近错误" min-width="200" show-overflow-tooltip />
      <el-table-column label="操作" width="120" fixed="right">
        <template #default="{ row }">
          <TableActionButton :icon="Trash2" label="吊销" danger @click="revoke(row)" />
        </template>
      </el-table-column>
    </el-table>
  </section>
</template>

<style scoped>
.cluster-nodes {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.page-head h1 {
  margin: 0 0 4px;
  font-size: 18px;
}

.hint {
  margin: 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.node-name {
  display: flex;
  align-items: center;
  gap: 6px;
}
</style>
