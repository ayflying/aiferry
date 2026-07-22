<script setup lang="ts">
import { Eye, Pencil, Plus, RefreshCw, Trash2 } from '@lucide/vue'

import type { ChannelType } from '../api/types'
import { channelTypeCostLabel } from '../lib/channelTypeDisplay'
import MobileRecordList from './MobileRecordList.vue'
import NameCodeCell from './NameCodeCell.vue'
import ResponsiveList from './ResponsiveList.vue'

const props = defineProps<{ loading: boolean; statusSaving: Record<number, boolean>; types: ChannelType[] }>()
const emit = defineEmits<{ create: []; edit: [channelType: ChannelType]; refresh: []; remove: [channelType: ChannelType]; 'set-status': [channelType: ChannelType, enabled: boolean] }>()
</script>

<template>
  <div class="page-toolbar"><div class="muted">JSON 定义模型发现、OpenAI 接口能力、鉴权和费用字段路径</div><div class="spacer" /><el-button :icon="RefreshCw" :loading="props.loading" @click="emit('refresh')">刷新</el-button><el-button type="primary" :icon="Plus" @click="emit('create')">添加渠道类型</el-button></div>
  <div class="table-panel">
    <ResponsiveList>
      <template #desktop>
        <el-table v-loading="props.loading" :data="props.types" row-key="id">
          <el-table-column label="类型" min-width="170"><template #default="{ row }"><NameCodeCell :name="row.name" :code="row.code" /></template></el-table-column>
          <el-table-column label="模型接口" min-width="220"><template #default="{ row }"><span class="mono">{{ row.config.models.method }} {{ row.config.models.path }}</span></template></el-table-column>
          <el-table-column label="余额查询" min-width="160"><template #default="{ row }"><span>{{ channelTypeCostLabel(row) }}</span><small v-if="row.config.costs.path"> · {{ row.config.costs.path }}</small></template></el-table-column>
          <el-table-column label="状态" width="142"><template #default="{ row }"><div class="type-status-control"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span><el-switch :model-value="row.status === 1" :disabled="row.builtIn === 1 || props.statusSaving[row.id]" :aria-label="`${row.name} ${row.status === 1 ? '已启用' : '已停用'}`" @update:model-value="emit('set-status', row, $event)" /></div></template></el-table-column>
          <el-table-column label="来源" width="88"><template #default="{ row }"><span class="muted">{{ row.builtIn === 1 ? '内置' : '自定义' }}</span></template></el-table-column>
          <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip :content="row.builtIn === 1 ? '查看内置配置' : '编辑类型'"><button class="icon-button" type="button" :aria-label="`${row.builtIn === 1 ? '查看' : '编辑'}渠道类型 ${row.name}`" @click="emit('edit', row)"><Eye v-if="row.builtIn === 1" :size="16" /><Pencil v-else :size="16" /></button></el-tooltip><el-tooltip v-if="row.builtIn === 0" content="删除类型"><button class="icon-button danger" type="button" :aria-label="`删除渠道类型 ${row.name}`" @click="emit('remove', row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column>
        </el-table>
      </template>
      <template #mobile>
        <MobileRecordList :loading="props.loading">
          <article v-for="row in props.types" :key="row.id" class="mobile-record">
            <div class="mobile-record__header"><div class="mobile-record__title"><strong>{{ row.name }}</strong><small class="mono">{{ row.code }} · {{ row.builtIn === 1 ? '内置类型' : '自定义类型' }}</small></div><div class="type-status-control"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span><el-switch :model-value="row.status === 1" :disabled="row.builtIn === 1 || props.statusSaving[row.id]" :aria-label="`${row.name} ${row.status === 1 ? '已启用' : '已停用'}`" @update:model-value="emit('set-status', row, $event)" /></div></div>
            <dl class="mobile-record__facts"><div class="mobile-record__wide"><dt>模型接口</dt><dd class="mono">{{ row.config.models.method }} {{ row.config.models.path }}</dd></div><div><dt>余额查询</dt><dd>{{ channelTypeCostLabel(row) }}<span v-if="row.config.costs.path"> · {{ row.config.costs.path }}</span></dd></div><div><dt>来源</dt><dd>{{ row.builtIn === 1 ? '内置' : '自定义' }}</dd></div></dl>
            <div class="mobile-record__footer"><span class="muted">渠道类型</span><div class="mobile-record__actions"><el-button size="small" :icon="row.builtIn === 1 ? Eye : Pencil" @click="emit('edit', row)">{{ row.builtIn === 1 ? '查看' : '编辑' }}</el-button><el-button v-if="row.builtIn === 0" size="small" :icon="Trash2" type="danger" plain @click="emit('remove', row)">删除</el-button></div></div>
          </article>
        </MobileRecordList>
      </template>
    </ResponsiveList>
    <div v-if="!props.loading && !props.types.length" class="empty-state"><div><strong>还没有渠道类型</strong><span>添加 JSON 配置后即可在渠道表单中选用</span></div></div>
  </div>
</template>

<style scoped>
.page-toolbar { display: flex; min-height: 36px; align-items: center; gap: 10px; margin-bottom: 18px; }.page-toolbar .spacer { flex: 1; }.type-status-control { display: inline-flex; align-items: center; gap: 10px; white-space: nowrap; }.type-status-control :deep(.el-switch) { flex: 0 0 auto; }.table-panel small { color: #7b8792; }
@media (max-width: 600px) { .page-toolbar { align-items: flex-start; flex-wrap: wrap; }.page-toolbar .spacer { display: none; } }
</style>
