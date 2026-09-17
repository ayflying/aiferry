<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronUp, Pencil, Plus, Trash2 } from '@lucide/vue'

import type { TimeWindow } from '../api/types'
import { createClosedWindow, createClosedWindows, describeTimeWindows, timeWindowListIsEmpty, windowRowsFromRecord, windowRowsToRecord } from '../lib/time-window'
import TableActionButton from './TableActionButton.vue'
import TimeWindowEditor from './TimeWindowEditor.vue'

/**
 * 渠道「关闭时间」页签：按「渠道 × 模型」手动添加定时关闭时段。
 *
 * 每个模型占一行，行内可配置多条规则（规则列表，任一命中即关闭）——
 * 例如「工作日白天关闭」与「周末全天关闭」就是同一模型下的两条规则。
 * 同一模型只能占一行，已选模型不再出现在其他行的下拉选项里。
 *
 * windows 的语义：键存在即「本次提交了该字段」——值为空数组表示清除，
 * 键不存在表示保持库里原值。因此删除一行时必须保留该键并置空；
 * 直接删键会被后端理解为「保持原值」，清除就不生效。
 */
const props = defineProps<{
  models: Array<{ publicName: string; upstreamName: string }>
  windows: Record<string, TimeWindow[]>
}>()

const emit = defineEmits<{ (event: 'update:windows', value: Record<string, TimeWindow[]>): void }>()

type WindowRow = { id: number; publicName: string; windows: TimeWindow[] }

const rows = ref<WindowRow[]>([])
const editingId = ref(0)
/** 被用户移除的既有键，提交时必须显式置空才算清除。 */
const clearedNames = new Set<string>()
let rowSeq = 0
/**
 * 上一次由本面板发出的负载对象，用于识别父级原样回灌的「回声」。
 *
 * 回声不能重建本地行：未选模型的新行不会进负载，重建会把它抹掉，正在编辑的时段也会被收起。
 * 但识别**只能成功一次**——父级每次载入都是「先清空、再回填」（见 ChannelsView.discover），
 * 若用「内容永久比较」的守卫，清空后的回填会因内容与上次提交相同而被继续当成回声吞掉，
 * 面板就会一直停在清空后的空状态：页签徽标显示已配置、面板却写「暂无定时关闭配置」，
 * 只有刷新页面才恢复。故这里记下回声后立即消费掉，此后一切外部赋值都按权威数据重建。
 */
let echoed: Record<string, TimeWindow[]> | null = null
let echoedDigest = ''

const configuredCount = computed(() => rows.value.filter((row) => row.publicName).length)

/** 稳定序列化：键排序后取参与判定的字段，避免比较时受对象键顺序影响。 */
function serialize(record: Record<string, TimeWindow[]>): string {
  return JSON.stringify(
    Object.keys(record)
      .sort()
      .map((name) => [name, record[name]?.map((window) => [window?.tz ?? '', window?.weekdays ?? [], window?.ranges ?? []]) ?? []]),
  )
}

function rowsFromWindows(source: Record<string, TimeWindow[]>): WindowRow[] {
  return windowRowsFromRecord(source).map((item) => ({ id: (rowSeq += 1), ...item }))
}

// 只有数据确实来自外部（切换渠道、重新打开弹窗）时才重建行；
// 父级原样回灌的自己的负载不动本地状态，否则新加的空行会被抹掉、编辑中的时段会被收起。
watch(() => props.windows, (next) => {
  const isEcho = echoed !== null && (next === echoed || serialize(next) === echoedDigest)
  echoed = null
  echoedDigest = ''
  if (isEcho) return
  rows.value = rowsFromWindows(next)
  clearedNames.clear()
  editingId.value = 0
}, { immediate: true, deep: true })

function commit() {
  const payload = windowRowsToRecord(rows.value, clearedNames)
  echoed = payload
  echoedDigest = serialize(payload)
  emit('update:windows', payload)
}

/** 下拉选项：排除已被其他行占用的模型；不在启用清单里的历史配置也要能回显，否则无法删除。 */
function optionsFor(row: WindowRow): Array<{ publicName: string; upstreamName: string }> {
  const taken = new Set(rows.value.filter((item) => item.id !== row.id && item.publicName).map((item) => item.publicName))
  const options = props.models.filter((item) => !taken.has(item.publicName))
  if (row.publicName && !options.some((item) => item.publicName === row.publicName)) {
    options.unshift({ publicName: row.publicName, upstreamName: row.publicName })
  }
  return options
}

function addRow() {
  const row: WindowRow = { id: (rowSeq += 1), publicName: '', windows: createClosedWindows() }
  rows.value = [...rows.value, row]
  editingId.value = row.id
  commit()
}

function removeRow(row: WindowRow) {
  if (row.publicName) clearedNames.add(row.publicName)
  rows.value = rows.value.filter((item) => item.id !== row.id)
  if (editingId.value === row.id) editingId.value = 0
  commit()
}

function updateRowModel(row: WindowRow, value: unknown) {
  const next = typeof value === 'string' ? value.trim() : ''
  // 改选模型等于删掉旧配置、新增新配置：旧键置空，新键从待清除集合里摘掉。
  if (row.publicName && row.publicName !== next) clearedNames.add(row.publicName)
  if (next) clearedNames.delete(next)
  row.publicName = next
  commit()
}

function addRule(row: WindowRow) {
  row.windows = [...row.windows, createClosedWindow()]
  commit()
}

function removeRule(row: WindowRow, index: number) {
  row.windows = row.windows.filter((_, current) => current !== index)
  commit()
}

function updateRule(row: WindowRow, index: number, value: TimeWindow) {
  row.windows = row.windows.map((item, current) => (current === index ? value : item))
  commit()
}

function toggleEditing(row: WindowRow) {
  editingId.value = editingId.value === row.id ? 0 : row.id
}

function summaryOf(row: WindowRow): string {
  return timeWindowListIsEmpty(row.windows) ? '未设置时段' : describeTimeWindows(row.windows)
}
</script>

<template>
  <div class="window-panel">
    <p class="panel-hint">
      手动添加需要定时关闭的模型并设置时段：这些模型在时段内不再通过本渠道提供服务，时段过去自动恢复。
      同一模型可配置多条规则（任一命中即关闭），例如「工作日白天关闭」再加「周末全天关闭」。
      <strong>只影响本渠道</strong>：其他渠道仍可正常服务同一个模型。
    </p>

    <div v-if="!models.length" class="panel-empty">当前没有启用的模型，请先在「选择模型」页签勾选模型。</div>

    <template v-else>
      <div class="panel-toolbar">
        <span class="panel-count">已配置 {{ configuredCount }} 条关闭时段</span>
        <el-button type="primary" :icon="Plus" @click="addRow">添加关闭时段</el-button>
      </div>

      <div v-if="rows.length" class="window-list">
        <div v-for="row in rows" :key="row.id" class="window-entry">
          <div class="window-entry__row">
            <el-select
              :model-value="row.publicName"
              filterable
              clearable
              placeholder="选择要定时关闭的模型"
              class="window-entry__model"
              @update:model-value="updateRowModel(row, $event)"
            >
              <el-option v-for="item in optionsFor(row)" :key="item.publicName" :label="item.publicName" :value="item.publicName">
                <span class="window-option__name">{{ item.publicName }}</span>
                <span v-if="item.upstreamName !== item.publicName" class="window-option__upstream">{{ item.upstreamName }}</span>
              </el-option>
            </el-select>
            <span class="window-entry__summary" :class="{ empty: timeWindowListIsEmpty(row.windows) }">{{ summaryOf(row) }}</span>
            <!-- 与旁边的删除按钮同为一枚图标按钮：展开时换成上箭头，补回文字按钮原有的「收起」提示。 -->
            <TableActionButton
              :icon="editingId === row.id ? ChevronUp : Pencil"
              :label="editingId === row.id ? '收起时段' : '编辑时段'"
              :size="15"
              :disabled="!row.publicName"
              @click="toggleEditing(row)"
            />
            <TableActionButton :icon="Trash2" label="删除关闭时段" danger :size="15" @click="removeRow(row)" />
          </div>
          <div v-if="editingId === row.id" class="window-entry__editor">
            <div v-for="(rule, index) in row.windows" :key="`rule-${row.id}-${index}`" class="rule-card">
              <div class="rule-card__head">
                <span class="rule-card__label">规则 {{ index + 1 }}</span>
                <TableActionButton
                  v-if="row.windows.length > 1"
                  :icon="Trash2"
                  :label="`删除规则 ${index + 1}`"
                  danger
                  :size="14"
                  @click="removeRule(row, index)"
                />
              </div>
              <TimeWindowEditor :model-value="rule" :show-summary="false" @update:model-value="updateRule(row, index, $event)" />
            </div>
            <el-button size="small" :icon="Plus" class="rule-add" @click="addRule(row)">添加规则</el-button>
          </div>
        </div>
      </div>
      <div v-else class="panel-empty">暂无定时关闭配置</div>
    </template>
  </div>
</template>

<style scoped>
.window-panel { display: grid; gap: 10px; }.panel-hint { margin: 0; color: #66717d; font-size: 11px; line-height: 1.6; }.panel-hint strong { color: #33404c; }.panel-empty { padding: 14px; border: 1px dashed #dce2e7; border-radius: 6px; color: #66717d; font-size: 12px; text-align: center; }.panel-toolbar { display: flex; min-height: 36px; align-items: center; justify-content: space-between; gap: 12px; }.panel-count { color: #66717d; font-size: 12px; }.window-list { display: grid; gap: 7px; max-height: 420px; overflow-y: auto; }.window-entry { display: grid; gap: 8px; padding: 9px 11px; border: 1px solid #dce2e7; border-radius: 6px; }.window-entry__row { display: grid; grid-template-columns: minmax(180px, 1.1fr) minmax(140px, 1fr) 34px 34px; align-items: center; gap: 10px; }.window-entry__model { min-width: 0; }.window-option__name { font-family: 'JetBrains Mono', monospace; font-size: 12px; }.window-option__upstream { margin-left: 8px; color: #8b959e; font-size: 11px; }.window-entry__summary { overflow: hidden; color: #b45309; font-size: 11px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }.window-entry__summary.empty { color: #8b959e; font-weight: 400; }.window-entry__editor { display: grid; gap: 8px; padding: 9px; border: 1px solid #e3e8ec; border-radius: 6px; background: #fbfcfd; }.rule-card { display: grid; gap: 8px; padding: 9px; border: 1px solid #e3e8ec; border-radius: 6px; background: #fff; }.rule-card__head { display: flex; min-height: 20px; align-items: center; justify-content: space-between; gap: 8px; }.rule-card__label { color: #4b5763; font-size: 11px; font-weight: 600; }.rule-add { justify-self: start; }
@media (max-width: 600px) { .window-entry__row { grid-template-columns: minmax(0, 1fr) 34px 34px; }.window-entry__model { grid-column: 1 / -1; } }
</style>
