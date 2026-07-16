<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Coins, RefreshCw } from '@lucide/vue'
import { ElMessage } from 'element-plus'
import { apiGet, apiPut } from '../api/client'
import type { ChannelModel } from '../api/types'
import { useAppStore } from '../stores/app'
import { compareModelNames } from '../lib/models'

const store = useAppStore()
const models = ref<ChannelModel[]>([])
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')
const channelId = ref<number>()
const enabledFilter = ref<string>('all')
const editOpen = ref(false)
const current = ref<ChannelModel>()
const priceDrawerSize = window.innerWidth <= 600 ? '94%' : '520px'
const form = reactive({ inputPrice: undefined as number | undefined, cachedInputPrice: undefined as number | undefined, outputPrice: undefined as number | undefined })

const filtered = computed(() => models.value.filter((item) => {
  if (channelId.value && item.channelId !== channelId.value) return false
  if (enabledFilter.value === 'enabled' && item.enabled !== 1) return false
  if (enabledFilter.value === 'disabled' && item.enabled === 1) return false
  const query = keyword.value.trim().toLowerCase()
  return !query || item.publicName.toLowerCase().includes(query) || item.upstreamName.toLowerCase().includes(query)
}).sort((left, right) => compareModelNames(left.publicName, right.publicName)))

async function load() {
  loading.value = true
  try {
    const [items] = await Promise.all([apiGet<ChannelModel[]>('/models'), store.loadChannels()])
    models.value = items
  } catch (error) { ElMessage.error((error as Error).message) } finally { loading.value = false }
}

function openEdit(model: ChannelModel) {
  current.value = model
  Object.assign(form, {
    inputPrice: model.inputPrice, cachedInputPrice: model.cachedInputPrice, outputPrice: model.outputPrice,
  })
  editOpen.value = true
}

async function save() {
  if (!current.value) return
  saving.value = true
  try {
    await apiPut(`/models/${current.value.id}`, {
      publicName: current.value.publicName,
      upstreamName: current.value.upstreamName,
      enabled: current.value.enabled === 1,
      ...form,
    })
    ElMessage.success('模型价格已保存')
    editOpen.value = false
    await load()
  } catch (error) { ElMessage.error((error as Error).message) } finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar">
      <div class="toolbar-group">
        <el-input v-model="keyword" clearable placeholder="搜索模型" style="width: 220px" />
        <el-select v-model="channelId" clearable placeholder="全部渠道" style="width: 170px"><el-option v-for="item in store.channels" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-select v-model="enabledFilter" style="width: 130px"><el-option label="全部状态" value="all" /><el-option label="已启用" value="enabled" /><el-option label="未启用" value="disabled" /></el-select>
      </div>
      <div class="spacer" />
      <el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="filtered" row-key="id">
        <el-table-column prop="publicName" label="公开模型" min-width="190"><template #default="{ row }"><span class="mono model-name">{{ row.publicName }}</span></template></el-table-column>
        <el-table-column prop="upstreamName" label="上游模型" min-width="190"><template #default="{ row }"><span class="mono muted">{{ row.upstreamName }}</span></template></el-table-column>
        <el-table-column prop="channelName" label="渠道" min-width="140" />
        <el-table-column label="状态" width="96"><template #default="{ row }"><span class="status-dot" :class="row.enabled === 1 ? 'success' : ''">{{ row.enabled === 1 ? '已启用' : '未启用' }}</span></template></el-table-column>
        <el-table-column label="价格 / 1M Token" min-width="230"><template #default="{ row }"><div class="price-line"><span>入 {{ row.inputPrice ?? '—' }}</span><span>缓存 {{ row.cachedInputPrice ?? '—' }}</span><span>出 {{ row.outputPrice ?? '—' }}</span></div></template></el-table-column>
        <el-table-column label="操作" width="86" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip content="设置价格"><button class="icon-button" type="button" :aria-label="`设置 ${row.publicName} 的价格`" @click="openEdit(row)"><Coins :size="16" /></button></el-tooltip></div></template></el-table-column>
      </el-table>
      <div v-if="!loading && !filtered.length" class="empty-state"><div><strong>没有匹配模型</strong><span>先在渠道页发现并选择模型</span></div></div>
    </div>

    <el-drawer v-model="editOpen" title="价格设置" :size="priceDrawerSize">
      <el-form label-position="top">
        <div class="price-target"><div><span>公开模型</span><code>{{ current?.publicName }}</code></div><div><span>渠道</span><strong>{{ current?.channelName }}</strong></div></div>
        <div class="section-heading price-heading"><h2>USD / 1M Token</h2><span>留空表示未定价</span></div>
        <div class="form-grid">
          <el-form-item label="输入价格"><el-input-number v-model="form.inputPrice" :min="0" :precision="6" :controls="false" placeholder="未定价" /></el-form-item>
          <el-form-item label="缓存输入价格"><el-input-number v-model="form.cachedInputPrice" :min="0" :precision="6" :controls="false" placeholder="默认输入价" /></el-form-item>
          <el-form-item label="输出价格"><el-input-number v-model="form.outputPrice" :min="0" :precision="6" :controls="false" placeholder="未定价" /></el-form-item>
        </div>
      </el-form>
      <template #footer><el-button @click="editOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存价格</el-button></template>
    </el-drawer>
  </div>
</template>

<style scoped>
.model-name { color: #15202b; font-weight: 600; }.price-line { display: flex; gap: 12px; color: #4b5763; font-family: 'JetBrains Mono', monospace; font-size: 10px; }.price-target { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; padding: 13px; border: 1px solid #dce2e7; border-radius: 6px; background: #f7f9fa; }.price-target div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }.price-target span { color: #66717d; font-size: 11px; }.price-target code, .price-target strong { overflow: hidden; font-family: 'JetBrains Mono', monospace; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }.price-heading { margin-top: 20px; padding-top: 16px; border-top: 1px solid #dce2e7; }
</style>
