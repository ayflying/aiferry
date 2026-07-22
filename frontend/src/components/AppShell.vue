<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  Cable,
  ChartNoAxesCombined,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Gauge,
  KeyRound,
  LogOut,
  Menu,
  Settings,
  Ticket,
  UserRound,
  UsersRound,
} from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { showError } from '../lib/error'
import SiteFooter from './SiteFooter.vue'
import { useSystemStore } from '../stores/system'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const system = useSystemStore()
const collapsed = ref(localStorage.getItem('aiferry-sidebar') === 'collapsed')
const mobile = ref(false)
const mobileOpen = ref(false)

type NavigationItem = {
  path: string
  label: string
  icon: Component
  children?: Array<Pick<NavigationItem, 'path' | 'label'>>
}

const adminItems: NavigationItem[] = [
  { path: '/', label: '仪表盘', icon: Gauge },
  { path: '/api-keys', label: '访问密钥', icon: KeyRound },
  { path: '/usage', label: '用量日志', icon: Activity },
  {
    path: '/channels',
    label: '渠道管理',
    icon: Cable,
    children: [
      { path: '/channels', label: '渠道' },
      { path: '/channels/groups', label: '渠道分组' },
      { path: '/channels/types', label: '渠道类型' },
    ],
  },
  { path: '/models', label: '模型与价格', icon: ChartNoAxesCombined },
  { path: '/users', label: '用户管理', icon: UsersRound },
  { path: '/redemption-codes', label: '兑换码', icon: Ticket },
  {
    path: '/settings',
    label: '系统设置',
    icon: Settings,
    children: [
      { path: '/settings', label: '运行概览' },
      { path: '/settings/basic', label: '基础设置' },
      { path: '/settings/resilience', label: '路由可靠性' },
      { path: '/settings/security', label: '安全与限制' },
      { path: '/settings/mail', label: '邮件提醒' },
    ],
  },
]
const items = computed<NavigationItem[]>(() => auth.user?.isAdmin
  ? adminItems
  : [
      { path: '/profile', label: '个人中心', icon: UserRound },
      { path: '/api-keys', label: '访问密钥', icon: KeyRound },
      { path: '/usage', label: '用量日志', icon: Activity },
      { path: '/models', label: '模型与价格', icon: ChartNoAxesCombined },
    ])

const expandedGroupPath = ref(items.value.find((item) => hasActiveChild(item))?.path)

const pageTitle = computed(() => String(route.meta.title || system.systemName))

function isActive(path: string) {
  return route.path === path
}

function hasActiveChild(item: NavigationItem) {
  return item.children?.some((child) => isActive(child.path)) === true
}

function isExpanded(item: NavigationItem) {
  return expandedGroupPath.value === item.path
}

function toggleNavigationGroup(item: NavigationItem) {
  expandedGroupPath.value = isExpanded(item) ? undefined : item.path
}

watch(() => route.path, () => {
  const activeGroup = items.value.find((item) => hasActiveChild(item))
  expandedGroupPath.value = activeGroup?.path
})

function toggleSidebar() {
  collapsed.value = !collapsed.value
  localStorage.setItem('aiferry-sidebar', collapsed.value ? 'collapsed' : 'expanded')
}

function navigate(path: string) {
  const parent = items.value.find((item) => item.children?.some((child) => child.path === path))
  expandedGroupPath.value = parent?.path
  router.push(path)
  mobileOpen.value = false
}

function updateViewport() {
  mobile.value = window.innerWidth < 900
  if (!mobile.value) mobileOpen.value = false
}

function resetLogo(event: Event) {
  ;(event.target as HTMLImageElement).src = '/aiferry-logo.png'
}

async function handleUserCommand(command: string) {
  if (command === 'profile') {
    await router.push('/profile')
    return
  }
  if (command !== 'logout') return
  try {
    await auth.logout()
    await router.replace('/login')
  } catch (error) {
    showError(error, '退出登录失败')
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
        <span class="brand-mark" role="img" :aria-label="system.systemName"><img class="brand-logo" :src="system.logoUrl" alt="" @error="resetLogo" /></span>
        <div v-if="!collapsed" class="brand-copy">
          <strong>{{ system.systemName }}</strong>
          <span>AI 网关</span>
        </div>
      </div>

      <nav class="nav-list" aria-label="主导航">
        <template v-for="item in items" :key="item.path">
          <el-tooltip v-if="!item.children || collapsed" :disabled="!collapsed" :content="item.label" placement="right">
            <button class="nav-item" :class="{ active: isActive(item.path), 'active-branch': hasActiveChild(item) && !isActive(item.path) }" type="button" @click="item.children && !collapsed ? toggleNavigationGroup(item) : navigate(item.path)">
              <component :is="item.icon" :size="19" />
              <span v-if="!collapsed">{{ item.label }}</span>
            </button>
          </el-tooltip>
          <div v-else class="nav-group">
            <button class="nav-item nav-group-toggle" :class="{ 'active-branch': hasActiveChild(item), expanded: isExpanded(item) }" type="button" :aria-expanded="isExpanded(item)" @click="toggleNavigationGroup(item)">
              <component :is="item.icon" :size="19" />
              <span>{{ item.label }}</span>
              <ChevronDown class="nav-group-arrow" :class="{ expanded: isExpanded(item) }" :size="16" />
            </button>
            <div v-show="isExpanded(item)" class="nav-sublist">
              <button v-for="child in item.children" :key="child.path" class="nav-subitem" :class="{ active: isActive(child.path) }" type="button" @click="navigate(child.path)">{{ child.label }}</button>
            </div>
          </div>
        </template>
      </nav>

      <button class="sidebar-toggle" type="button" :aria-label="collapsed ? '展开侧栏' : '收起侧栏'" @click="toggleSidebar">
        <ChevronRight v-if="collapsed" :size="18" />
        <ChevronLeft v-else :size="18" />
      </button>
    </aside>

    <el-drawer v-model="mobileOpen" direction="ltr" size="260px" :with-header="false" class="mobile-drawer">
      <div class="brand">
        <span class="brand-mark" role="img" :aria-label="system.systemName"><img class="brand-logo" :src="system.logoUrl" alt="" @error="resetLogo" /></span>
        <div class="brand-copy"><strong>{{ system.systemName }}</strong><span>AI 网关</span></div>
      </div>
      <nav class="nav-list" aria-label="主导航">
        <template v-for="item in items" :key="item.path">
          <button v-if="!item.children" class="nav-item" :class="{ active: isActive(item.path) }" type="button" @click="navigate(item.path)">
            <component :is="item.icon" :size="19" />
            <span>{{ item.label }}</span>
          </button>
          <div v-else class="nav-group">
            <button class="nav-item nav-group-toggle" :class="{ 'active-branch': hasActiveChild(item), expanded: isExpanded(item) }" type="button" :aria-expanded="isExpanded(item)" @click="toggleNavigationGroup(item)">
              <component :is="item.icon" :size="19" />
              <span>{{ item.label }}</span>
              <ChevronDown class="nav-group-arrow" :class="{ expanded: isExpanded(item) }" :size="16" />
            </button>
            <div v-show="isExpanded(item)" class="nav-sublist">
              <button v-for="child in item.children" :key="child.path" class="nav-subitem" :class="{ active: isActive(child.path) }" type="button" @click="navigate(child.path)">{{ child.label }}</button>
            </div>
          </div>
        </template>
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
            <span class="user-copy"><strong>{{ auth.user?.name || '用户' }}</strong><small>{{ auth.user?.isAdmin ? '管理员' : '用户' }}</small></span>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="profile"><UserRound :size="16" />个人中心</el-dropdown-item>
              <el-dropdown-item command="logout"><LogOut :size="16" />退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </header>
      <div class="page-content">
        <router-view />
      </div>
      <SiteFooter />
    </main>
  </div>
</template>
