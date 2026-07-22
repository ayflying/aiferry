<script setup lang="ts">
import { Coins, FlaskConical, KeyRound, LoaderCircle, Pencil, Plus, RefreshCw, ScanSearch, Trash2 } from '@lucide/vue'

import type { Channel } from '../api/types'
import { channelStatusLabel, isChannelEnabled } from '../lib/channelDisplay'
import { formatCost, formatLatency, formatTime } from '../lib/format'
import MobileRecordList from './MobileRecordList.vue'
import ResponsiveList from './ResponsiveList.vue'

const props = defineProps<{
  channels: Channel[]
  loading: boolean
  queryingCostID?: number
  statusSaving: Record<number, boolean>
}>()

const emit = defineEmits<{
  create: []
  discover: [channel: Channel]
  edit: [channel: Channel]
  'open-credentials': [channel: Channel]
  queryCost: [channel: Channel]
  refresh: []
  remove: [channel: Channel]
  'set-status': [channel: Channel, enabled: boolean]
  test: [channel: Channel]
}>()
</script>

<template>
  <div class="page-toolbar">
    <div class="muted">管理上游、模型选择、路由顺序和费用查询</div>
    <div class="spacer" />
    <el-button :icon="RefreshCw" :loading="props.loading" @click="emit('refresh')">刷新</el-button>
    <el-button type="primary" :icon="Plus" @click="emit('create')">添加渠道</el-button>
  </div>

  <div class="table-panel">
    <ResponsiveList>
      <template #desktop>
        <el-table v-loading="props.loading" :data="props.channels" row-key="id">
          <el-table-column label="渠道" min-width="180">
            <template #default="{ row }">
              <div class="channel-name"><strong>{{ row.name }}</strong><span>{{ row.baseUrl }}</span></div>
            </template>
          </el-table-column>
          <el-table-column label="类型" min-width="120">
            <template #default="{ row }">
              <div class="channel-name"><strong>{{ row.typeName }}</strong><span>{{ row.type }}</span></div>
            </template>
          </el-table-column>
          <el-table-column label="状态" min-width="214">
            <template #default="{ row }">
              <div class="channel-status-control">
                <el-tooltip v-if="row.autoDisabled" :content="row.autoDisabledReason || '渠道被自动禁用'" placement="top"><span class="status-dot warning">{{ channelStatusLabel(row) }}</span></el-tooltip>
                <el-tooltip v-else-if="row.credentialsUnavailable && row.status === 1" content="该渠道当前不可路由：所有上游密钥均不可用。开启渠道会恢复全部上游密钥。" placement="top"><span class="status-dot warning">{{ channelStatusLabel(row) }}</span></el-tooltip>
                <span v-else class="status-dot" :class="isChannelEnabled(row) ? 'success' : ''">{{ channelStatusLabel(row) }}</span>
                <el-switch :model-value="isChannelEnabled(row)" :loading="props.statusSaving[row.id]" :disabled="props.statusSaving[row.id]" :aria-label="`${row.name} ${isChannelEnabled(row) ? '已启用' : '已停用'}`" @update:model-value="emit('set-status', row, $event)" />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="路由" width="108"><template #default="{ row }"><span class="mono">P{{ row.priority }} / W{{ row.weight }}</span></template></el-table-column>
          <el-table-column label="模型" width="100"><template #default="{ row }">{{ row.enabledModelCount }} / {{ row.discoveredModels }}</template></el-table-column>
          <el-table-column label="最近测试" min-width="130"><template #default="{ row }"><span v-if="row.lastTestStatus" class="status-dot" :class="row.lastTestStatus">{{ row.lastTestStatus === 'success' ? formatLatency(row.lastTestLatencyMs) : '失败' }}</span><span v-else class="muted">未测试</span></template></el-table-column>
          <el-table-column label="上游费用 / 余额" min-width="168">
            <template #default="{ row }">
              <button class="cost-link" type="button" @click="emit('open-credentials', row)">
                <div v-if="row.costSummaries?.length" class="cost-cell">
                  <template v-for="summary in row.costSummaries" :key="summary.currency"><span v-if="summary.usedAmount !== undefined">{{ summary.currency }} 已用 {{ formatCost(summary.usedAmount, summary.currency) }}</span><span v-if="summary.remainingAmount !== undefined">{{ summary.currency }} 余额 {{ formatCost(summary.remainingAmount, summary.currency) }}</span></template>
                  <small v-if="row.lastCostAt">{{ formatTime(row.lastCostAt) }}</small>
                </div>
                <span v-else class="muted">查看明细</span>
              </button>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="260" fixed="right" align="right">
            <template #default="{ row }">
              <div class="table-actions">
                <el-tooltip content="管理上游密钥"><button class="icon-button" type="button" :aria-label="`管理 ${row.name} 的上游密钥`" @click="emit('open-credentials', row)"><KeyRound :size="16" /></button></el-tooltip>
                <el-tooltip content="发现模型"><button class="icon-button" type="button" :aria-label="`发现 ${row.name} 的模型`" @click="emit('discover', row)"><ScanSearch :size="16" /></button></el-tooltip>
                <el-tooltip content="测试模型"><button class="icon-button" type="button" :aria-label="`测试 ${row.name} 的模型`" @click="emit('test', row)"><FlaskConical :size="16" /></button></el-tooltip>
                <el-tooltip :content="props.queryingCostID === row.id ? '正在查询费用' : '查询费用'"><button class="icon-button" type="button" :aria-label="`${props.queryingCostID === row.id ? '正在查询' : '查询'} ${row.name} 的费用`" :disabled="row.costQueryMode === 'none' || props.queryingCostID !== undefined" @click="emit('queryCost', row)"><LoaderCircle v-if="props.queryingCostID === row.id" :size="16" class="cost-query-spinner" /><Coins v-else :size="16" /></button></el-tooltip>
                <el-tooltip content="编辑"><button class="icon-button" type="button" :aria-label="`编辑渠道 ${row.name}`" @click="emit('edit', row)"><Pencil :size="16" /></button></el-tooltip>
                <el-tooltip content="删除"><button class="icon-button danger" type="button" :aria-label="`删除渠道 ${row.name}`" @click="emit('remove', row)"><Trash2 :size="16" /></button></el-tooltip>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </template>
      <template #mobile>
        <MobileRecordList :loading="props.loading">
          <article v-for="row in props.channels" :key="row.id" class="mobile-record">
            <div class="mobile-record__header">
              <div class="mobile-record__title"><strong>{{ row.name }}</strong><small>{{ row.baseUrl }}</small></div>
              <div class="channel-status-control">
                <el-tooltip v-if="row.autoDisabled" :content="row.autoDisabledReason || '渠道被自动禁用'" placement="top"><span class="status-dot warning">{{ channelStatusLabel(row) }}</span></el-tooltip>
                <el-tooltip v-else-if="row.credentialsUnavailable && row.status === 1" content="所有上游密钥均不可用。开启渠道会恢复全部上游密钥。" placement="top"><span class="status-dot warning">{{ channelStatusLabel(row) }}</span></el-tooltip>
                <span v-else class="status-dot" :class="isChannelEnabled(row) ? 'success' : ''">{{ channelStatusLabel(row) }}</span>
                <el-switch :model-value="isChannelEnabled(row)" :loading="props.statusSaving[row.id]" :disabled="props.statusSaving[row.id]" :aria-label="`${row.name} ${isChannelEnabled(row) ? '已启用' : '已停用'}`" @update:model-value="emit('set-status', row, $event)" />
              </div>
            </div>
            <dl class="mobile-record__facts">
              <div><dt>渠道类型</dt><dd>{{ row.typeName }} · <span class="mono">{{ row.type }}</span></dd></div>
              <div><dt>路由</dt><dd class="mono">P{{ row.priority }} / W{{ row.weight }}</dd></div>
              <div><dt>模型</dt><dd>{{ row.enabledModelCount }} / {{ row.discoveredModels }}</dd></div>
              <div><dt>最近测试</dt><dd>{{ row.lastTestStatus === 'success' ? formatLatency(row.lastTestLatencyMs) : row.lastTestStatus ? '失败' : '未测试' }}</dd></div>
              <div class="mobile-record__wide"><dt>上游费用 / 余额</dt><dd><button class="cost-link" type="button" @click="emit('open-credentials', row)"><span v-if="row.costSummaries?.length"><template v-for="summary in row.costSummaries" :key="summary.currency">{{ summary.currency }} 已用 {{ summary.usedAmount === undefined ? '—' : formatCost(summary.usedAmount, summary.currency) }} · 余额 {{ summary.remainingAmount === undefined ? '—' : formatCost(summary.remainingAmount, summary.currency) }} </template></span><span v-else class="muted">查看明细</span></button></dd></div>
            </dl>
            <div class="mobile-record__footer">
              <span class="muted">渠道操作</span>
              <div class="mobile-record__actions"><el-button size="small" :icon="KeyRound" @click="emit('open-credentials', row)">密钥</el-button><el-button size="small" :icon="ScanSearch" @click="emit('discover', row)">发现</el-button><el-button size="small" :icon="FlaskConical" @click="emit('test', row)">测试</el-button><el-button size="small" :icon="Coins" :loading="props.queryingCostID === row.id" :disabled="row.costQueryMode === 'none' || props.queryingCostID !== undefined" @click="emit('queryCost', row)">费用</el-button><el-button size="small" :icon="Pencil" @click="emit('edit', row)">编辑</el-button><el-button size="small" :icon="Trash2" type="danger" plain @click="emit('remove', row)">删除</el-button></div>
            </div>
          </article>
        </MobileRecordList>
      </template>
    </ResponsiveList>
    <div v-if="!props.loading && !props.channels.length" class="empty-state"><div><strong>还没有渠道</strong><span>先添加渠道类型，再接入第一个上游</span></div></div>
  </div>
</template>

<style scoped>
.page-toolbar { display: flex; min-height: 36px; align-items: center; gap: 10px; margin-bottom: 18px; }.page-toolbar .spacer { flex: 1; }
.channel-name { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.channel-name strong { font-size: 13px; }.channel-name span { overflow: hidden; color: #66717d; font-family: 'JetBrains Mono', monospace; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.channel-status-control { display: inline-flex; align-items: center; gap: 10px; white-space: nowrap; }.channel-status-control :deep(.el-switch) { flex: 0 0 auto; }
.cost-cell { display: flex; flex-direction: column; gap: 2px; font-size: 11px; }.cost-cell small, .table-panel small { color: #7b8792; }.cost-link { display: block; width: 100%; border: 0; padding: 0; text-align: left; color: inherit; background: transparent; cursor: pointer; }.cost-link:hover span { color: #1677ff; }
.cost-query-spinner { animation: cost-query-spin 0.9s linear infinite; }@keyframes cost-query-spin { to { transform: rotate(360deg); } }
@media (max-width: 600px) { .page-toolbar { align-items: flex-start; flex-wrap: wrap; }.page-toolbar .spacer { display: none; } }
@media (prefers-reduced-motion: reduce) { .cost-query-spinner { animation: none; } }
</style>
