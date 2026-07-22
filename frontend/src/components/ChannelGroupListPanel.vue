<script setup lang="ts">
import { Pencil, Plus, RefreshCw, Trash2 } from '@lucide/vue'

import type { Channel, ChannelGroup } from '../api/types'
import { channelNameForID } from '../lib/channelDisplay'
import MobileRecordList from './MobileRecordList.vue'
import NameCodeCell from './NameCodeCell.vue'
import ResponsiveList from './ResponsiveList.vue'

const props = defineProps<{ channels: Channel[]; groups: ChannelGroup[]; loading: boolean }>()
const emit = defineEmits<{ create: []; edit: [group: ChannelGroup]; refresh: []; remove: [group: ChannelGroup] }>()

function channelName(channelID: number) {
  return channelNameForID(props.channels, channelID)
}
</script>

<template>
  <div class="page-toolbar"><div class="muted">为密钥授权和路由策略维护渠道归属</div><div class="spacer" /><el-button :icon="RefreshCw" :loading="props.loading" @click="emit('refresh')">刷新</el-button><el-button type="primary" :icon="Plus" @click="emit('create')">添加分组</el-button></div>
  <div class="table-panel">
    <ResponsiveList>
      <template #desktop>
        <el-table v-loading="props.loading" :data="props.groups" row-key="id">
          <el-table-column label="分组" min-width="180"><template #default="{ row }"><NameCodeCell :name="row.name" :code="row.code" /></template></el-table-column>
          <el-table-column prop="description" label="说明" min-width="220" />
          <el-table-column label="渠道" min-width="180"><template #default="{ row }"><el-tag v-for="channelID in row.channelIds" :key="channelID" size="small" class="group-channel-tag">{{ channelName(channelID) }}</el-tag><span v-if="!row.channelIds.length" class="muted">未分配</span></template></el-table-column>
          <el-table-column label="状态" width="96"><template #default="{ row }"><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></template></el-table-column>
          <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><div class="table-actions"><el-tooltip content="编辑"><button class="icon-button" type="button" @click="emit('edit', row)"><Pencil :size="16" /></button></el-tooltip><el-tooltip content="删除"><button class="icon-button danger" type="button" @click="emit('remove', row)"><Trash2 :size="16" /></button></el-tooltip></div></template></el-table-column>
        </el-table>
      </template>
      <template #mobile>
        <MobileRecordList :loading="props.loading">
          <article v-for="row in props.groups" :key="row.id" class="mobile-record">
            <div class="mobile-record__header"><div class="mobile-record__title"><strong>{{ row.name }}</strong><small class="mono">{{ row.code }}</small></div><span class="status-dot" :class="row.status === 1 ? 'success' : ''">{{ row.status === 1 ? '启用' : '停用' }}</span></div>
            <dl class="mobile-record__facts"><div class="mobile-record__wide"><dt>说明</dt><dd>{{ row.description || '未填写说明' }}</dd></div><div class="mobile-record__wide"><dt>包含渠道</dt><dd><el-tag v-for="channelID in row.channelIds" :key="channelID" size="small" class="group-channel-tag">{{ channelName(channelID) }}</el-tag><span v-if="!row.channelIds.length" class="muted">未分配</span></dd></div></dl>
            <div class="mobile-record__footer"><span class="muted">渠道分组</span><div class="mobile-record__actions"><el-button size="small" :icon="Pencil" @click="emit('edit', row)">编辑</el-button><el-button size="small" :icon="Trash2" type="danger" plain @click="emit('remove', row)">删除</el-button></div></div>
          </article>
        </MobileRecordList>
      </template>
    </ResponsiveList>
    <div v-if="!props.loading && !props.groups.length" class="empty-state"><div><strong>还没有渠道分组</strong><span>创建分组后可对访问密钥限定可用渠道</span></div></div>
  </div>
</template>

<style scoped>
.page-toolbar { display: flex; min-height: 36px; align-items: center; gap: 10px; margin-bottom: 18px; }.page-toolbar .spacer { flex: 1; }.group-channel-tag { margin: 0 4px 4px 0; }
@media (max-width: 600px) { .page-toolbar { align-items: flex-start; flex-wrap: wrap; }.page-toolbar .spacer { display: none; } }
</style>
