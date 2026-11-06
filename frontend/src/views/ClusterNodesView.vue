<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RefreshCw, Server, Trash2, Zap } from '@lucide/vue'
import { ElMessageBox } from 'element-plus'
import { apiDelete, apiGet, apiPost } from '../api/client'
import type { ClusterNode, ClusterNodeList } from '../api/types'
import TableActionButton from '../components/TableActionButton.vue'
import { showError, showSuccess } from '../lib/error'
import { formatTime } from '../lib/format'

// 自动刷新间隔：在线状态是主节点内存里的实时值，不刷新就看不到掉线。
const AUTO_REFRESH_MS = 15000

const loading = ref(false)
const nodes = ref<ClusterNode[]>([])
const onlineCount = computed(() => nodes.value.filter((node) => node.online).length)
const pendingCount = computed(() => nodes.value.reduce((sum, node) => sum + (node.pendingRows || 0), 0))
let timer: ReturnType<typeof setInterval> | undefined

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

// 后台静默刷新：不显示 loading 遮罩，避免定时刷新让表格不停闪烁。
async function refreshQuietly() {
  try {
    const result = await apiGet<ClusterNodeList>('/cluster/nodes')
    nodes.value = result.nodes ?? []
  } catch {
    // 静默失败：保留上一次数据，用户可手动刷新查看真实错误。
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

// 通知节点立刻比对一次配置版本。只发一条通知，版本一致时节点不会重复拉快照。
async function syncNow(node: ClusterNode) {
  try {
    await apiPost<{ ok: boolean }>(`/cluster/nodes/${node.id}/sync`)
    showSuccess(`已通知 ${node.name} 立即同步`)
  } catch (error) {
    showError(error, '通知节点同步失败')
  }
}

function statusText(node: ClusterNode) {
  if (node.status !== 1) return '已吊销'
  if (!node.online) return '离线'
  return node.stale ? '在线·待同步' : '在线'
}

function statusTag(node: ClusterNode) {
  if (node.status !== 1) return 'info'
  if (!node.online) return 'danger'
  return node.stale ? 'warning' : 'success'
}

onMounted(() => {
  void load()
  timer = setInterval(refreshQuietly, AUTO_REFRESH_MS)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <section class="cluster-nodes">
    <header class="page-head">
      <div>
        <h1>集群节点</h1>
        <p class="hint">
          子节点通过 WebSocket 长连接订阅主节点，在线状态来自连接本身；配置变更经该连接即时下发，
          拉取配置与回传用量仍走原有接口。
        </p>
      </div>
      <el-button :loading="loading" @click="load">
        <RefreshCw :size="16" />
        <span>刷新</span>
      </el-button>
    </header>

    <div class="summary">
      <el-tag type="success" disable-transitions>在线 {{ onlineCount }}</el-tag>
      <el-tag type="info" disable-transitions>共 {{ nodes.length }}</el-tag>
      <el-tag :type="pendingCount > 0 ? 'warning' : 'info'" disable-transitions>
        待回传 {{ pendingCount }}
      </el-tag>
      <span class="hint">每 15 秒自动刷新</span>
    </div>

    <el-table v-loading="loading" :data="nodes" border stripe empty-text="还没有子节点注册">
      <el-table-column prop="name" label="节点" min-width="150">
        <template #default="{ row }">
          <div class="node-name">
            <Server :size="15" />
            <span>{{ row.name }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="statusTag(row)" disable-transitions>{{ statusText(row) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="连接时长" width="120">
        <template #default="{ row }">{{ row.online && row.streamSince ? formatTime(row.streamSince) : '—' }}</template>
      </el-table-column>
      <el-table-column prop="address" label="回调地址" min-width="170" show-overflow-tooltip />
      <el-table-column label="版本" width="110">
        <template #default="{ row }">{{ row.streamVersion || row.version || '—' }}</template>
      </el-table-column>
      <el-table-column prop="configVersion" label="配置版本" width="110" />
      <el-table-column label="待回传" width="100">
        <template #default="{ row }">{{ row.pendingRows }}</template>
      </el-table-column>
      <el-table-column label="最近心跳" width="170">
        <template #default="{ row }">{{ row.lastHeartbeat ? formatTime(row.lastHeartbeat) : '—' }}</template>
      </el-table-column>
      <el-table-column label="最近同步" width="170">
        <template #default="{ row }">{{ row.lastSync ? formatTime(row.lastSync) : '—' }}</template>
      </el-table-column>
      <el-table-column prop="lastError" label="最近错误" min-width="200" show-overflow-tooltip />
      <el-table-column label="操作" width="170" fixed="right">
        <template #default="{ row }">
          <div class="actions">
            <TableActionButton
              :icon="Zap"
              label="立即同步"
              :disabled="row.status !== 1"
              @click="syncNow(row)"
            />
            <TableActionButton :icon="Trash2" label="吊销" danger @click="revoke(row)" />
          </div>
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

.summary {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
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

.actions {
  display: flex;
  gap: 4px;
}
</style>
