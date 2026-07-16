<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  Cable,
  ChartNoAxesCombined,
  ChevronLeft,
  ChevronRight,
  Gauge,
  KeyRound,
  LogOut,
  Menu,
  Settings,
  ShipWheel,
  UserRound,
} from '@lucide/vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const collapsed = ref(localStorage.getItem('aiferry-sidebar') === 'collapsed')
const mobile = ref(false)
const mobileOpen = ref(false)

const items = [
  { path: '/', label: '仪表盘', icon: Gauge },
  { path: '/channels', label: '渠道', icon: Cable },
  { path: '/models', label: '模型与价格', icon: ChartNoAxesCombined },
  { path: '/api-keys', label: '访问密钥', icon: KeyRound },
  { path: '/usage', label: '用量', icon: Activity },
  { path: '/settings', label: '系统设置', icon: Settings },
]

const pageTitle = computed(() => String(route.meta.title || 'AiFerry'))

function toggleSidebar() {
  collapsed.value = !collapsed.value
  localStorage.setItem('aiferry-sidebar', collapsed.value ? 'collapsed' : 'expanded')
}

function navigate(path: string) {
  router.push(path)
  mobileOpen.value = false
}

function updateViewport() {
  mobile.value = window.innerWidth < 900
  if (!mobile.value) mobileOpen.value = false
}

async function handleUserCommand(command: string) {
  if (command !== 'logout') return
  try {
    await auth.logout()
    await router.replace('/login')
  } catch (error) {
    ElMessage.error((error as Error).message)
  }
}

onMounted(() => {
  updateViewport()
  window.addEventListener('resize', updateViewport)
})
onUnmounted(() => window.removeEventListener('resize', updateViewport))
</script>

<template>
  <div class="app-shell">
    <aside v-if="!mobile" class="sidebar" :class="{ collapsed }">
      <div class="brand" :class="{ compact: collapsed }">
        <span class="brand-mark"><ShipWheel :size="22" /></span>
        <div v-if="!collapsed" class="brand-copy">
          <strong>AiFerry</strong>
          <span>AI 摆渡</span>
        </div>
      </div>

      <nav class="nav-list" aria-label="主导航">
        <el-tooltip v-for="item in items" :key="item.path" :disabled="!collapsed" :content="item.label" placement="right">
          <button
            class="nav-item"
            :class="{ active: route.path === item.path }"
            type="button"
            @click="navigate(item.path)"
          >
            <component :is="item.icon" :size="19" />
            <span v-if="!collapsed">{{ item.label }}</span>
          </button>
        </el-tooltip>
      </nav>

      <button class="sidebar-toggle" type="button" :aria-label="collapsed ? '展开侧栏' : '收起侧栏'" @click="toggleSidebar">
        <ChevronRight v-if="collapsed" :size="18" />
        <ChevronLeft v-else :size="18" />
      </button>
    </aside>

    <el-drawer v-model="mobileOpen" direction="ltr" size="260px" :with-header="false" class="mobile-drawer">
      <div class="brand">
        <span class="brand-mark"><ShipWheel :size="22" /></span>
        <div class="brand-copy"><strong>AiFerry</strong><span>AI 摆渡</span></div>
      </div>
      <nav class="nav-list" aria-label="主导航">
        <button v-for="item in items" :key="item.path" class="nav-item" :class="{ active: route.path === item.path }" type="button" @click="navigate(item.path)">
          <component :is="item.icon" :size="19" />
          <span>{{ item.label }}</span>
        </button>
      </nav>
    </el-drawer>

    <main class="main-area">
      <header class="topbar">
        <button v-if="mobile" class="icon-button" type="button" aria-label="打开导航" @click="mobileOpen = true">
          <Menu :size="20" />
        </button>
        <h1>{{ pageTitle }}</h1>
        <div class="topbar-spacer" />
        <el-dropdown trigger="click" @command="handleUserCommand">
          <button class="user-menu" type="button" aria-label="用户菜单">
            <el-avatar :size="30" :src="auth.user?.avatarUrl || undefined">
              <UserRound :size="16" />
            </el-avatar>
            <span class="user-copy"><strong>{{ auth.user?.name || '用户' }}</strong><small>{{ auth.user?.role === 'admin' ? '管理员' : 'AI用户组' }}</small></span>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout"><LogOut :size="16" />退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </header>
      <div class="page-content">
        <router-view />
      </div>
    </main>
  </div>
</template>
