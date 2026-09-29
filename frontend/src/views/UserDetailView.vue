<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, RefreshCw, UserRound } from '@lucide/vue'
import { apiGet } from '../api/client'
import type { APIKey } from '../api/types'
import type { ManagedUser } from '../api/types'
import { showError } from '../lib/error'
import { formatBalance, formatTime } from '../lib/format'
import MobileRecordList from '../components/MobileRecordList.vue'
import ResponsiveList from '../components/ResponsiveList.vue'

const route = useRoute()
const router = useRouter()
const user = ref<ManagedUser>()
const keys = ref<APIKey[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const id = String(route.params.id)
    const [users, userKeys] = await Promise.all([
      apiGet<ManagedUser[]>('/users'),
      apiGet<APIKey[]>(`/api-keys?userId=${encodeURIComponent(id)}`),
    ])
    user.value = users.find((item) => String(item.id) === id)
    if (!user.value) throw new Error('用户不存在')
    keys.value = userKeys
  } catch (error) { showError(error, '加载用户详情失败') } finally { loading.value = false }
}

onMounted(load)
</script>

<template>
  <div class="page-stack">
    <div class="page-toolbar"><el-button :icon="ArrowLeft" @click="router.push('/users')">返回用户管理</el-button><div class="spacer" /><el-button :icon="RefreshCw" :loading="loading" @click="load">刷新</el-button></div>
    <section v-if="user" class="table-panel user-summary">
      <el-avatar :size="44" :src="user.avatarUrl || undefined"><UserRound :size="20" /></el-avatar>
      <div><h2>{{ user.nickname }}</h2><span class="muted">{{ user.role === 'admin' ? '管理员' : '用户' }} · {{ user.email || '未绑定邮箱' }}</span></div>
      <div class="spacer" /><div class="summary-balance"><small>账户余额</small><strong>{{ formatBalance(user.balance) }}</strong></div>
    </section>
    <section class="table-panel">
      <div class="section-heading"><div><h2>访问密钥</h2><p class="muted">当前用户申请的访问密钥，共 {{ keys.length }} 个</p></div></div>
      <ResponsiveList>
        <template #desktop><el-table v-loading="loading" :data="keys" row-key="id">
          <el-table-column label="名称" prop="name" min-width="170" />
          <el-table-column label="密钥" min-width="180"><template #default="{ row }"><span class="mono">{{ row.keyPrefix }}••••</span></template></el-table-column>
          <el-table-column label="状态" width="100"><template #default="{ row }"><el-tag :type="row.status === 1 ? 'success' : 'info'">{{ row.status === 1 ? '启用' : '停用' }}</el-tag></template></el-table-column>
          <el-table-column label="已用额度" min-width="120" align="right"><template #default="{ row }">{{ formatBalance(row.spentAmount) }}</template></el-table-column>
          <el-table-column label="额度上限" min-width="120" align="right"><template #default="{ row }">{{ row.spendLimit == null ? '不限' : formatBalance(row.spendLimit) }}</template></el-table-column>
          <el-table-column label="每日上限" min-width="120" align="right"><template #default="{ row }">{{ row.dailySpendLimit == null ? '不限' : formatBalance(row.dailySpendLimit) }}</template></el-table-column>
          <el-table-column label="创建时间" min-width="170"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
          <el-table-column label="最近使用" min-width="170"><template #default="{ row }">{{ row.lastUsedAt ? formatTime(row.lastUsedAt) : '从未使用' }}</template></el-table-column>
        </el-table></template>
        <template #mobile><MobileRecordList :loading="loading"><article v-for="key in keys" :key="key.id" class="mobile-record">
          <div class="mobile-record__header"><div class="mobile-record__title"><strong>{{ key.name }}</strong><small class="mono">{{ key.keyPrefix }}••••</small></div><el-tag :type="key.status === 1 ? 'success' : 'info'">{{ key.status === 1 ? '启用' : '停用' }}</el-tag></div>
          <dl class="mobile-record__facts"><div><dt>已用额度</dt><dd>{{ formatBalance(key.spentAmount) }}</dd></div><div><dt>额度上限</dt><dd>{{ key.spendLimit == null ? '不限' : formatBalance(key.spendLimit) }}</dd></div><div><dt>每日上限</dt><dd>{{ key.dailySpendLimit == null ? '不限' : formatBalance(key.dailySpendLimit) }}</dd></div><div class="mobile-record__wide"><dt>创建时间 / 最近使用</dt><dd>{{ formatTime(key.createdAt) }} / {{ key.lastUsedAt ? formatTime(key.lastUsedAt) : '从未使用' }}</dd></div></dl>
        </article></MobileRecordList></template>
      </ResponsiveList>
      <div v-if="!loading && !keys.length" class="empty-state"><strong>该用户尚未申请访问密钥</strong></div>
    </section>
  </div>
</template>

<style scoped>
.user-summary { display: flex; align-items: center; gap: 14px; padding: 18px; }
.user-summary h2, .section-heading h2 { margin: 0 0 5px; font-size: 16px; }
.summary-balance { display: flex; flex-direction: column; gap: 4px; text-align: right; }
.summary-balance small, .section-heading p { margin: 0; color: #7b8792; font-size: 12px; }
.section-heading { padding: 16px 18px; }
.empty-state { padding: 28px; text-align: center; color: #7b8792; }
</style>
