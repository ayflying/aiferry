import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { localReturnTo } from '../lib/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('../views/LoginView.vue'), meta: { title: '登录', public: true } },
    { path: '/', name: 'dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '仪表盘', admin: true } },
    { path: '/channels', name: 'channels', component: () => import('../views/ChannelsView.vue'), meta: { title: '渠道', admin: true } },
    { path: '/models', name: 'models', component: () => import('../views/ModelsView.vue'), meta: { title: '模型与价格' } },
    { path: '/api-keys', name: 'api-keys', component: () => import('../views/ApiKeysView.vue'), meta: { title: '访问密钥' } },
    { path: '/usage', name: 'usage', component: () => import('../views/UsageView.vue'), meta: { title: '用量日志' } },
    { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue'), meta: { title: '系统设置', admin: true } },
    { path: '/users', name: 'users', component: () => import('../views/UsersView.vue'), meta: { title: '用户管理', admin: true } },
    { path: '/redemption-codes', name: 'redemption-codes', component: () => import('../views/RedemptionCodesView.vue'), meta: { title: '兑换码', admin: true } },
    { path: '/profile', name: 'profile', component: () => import('../views/ProfileView.vue'), meta: { title: '个人中心' } },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.meta.public) {
    try {
      if (await auth.ensureUser()) return localReturnTo(to.query.returnTo)
    } catch {
      return true
    }
    return true
  }
  try {
    if (await auth.ensureUser()) {
      if (to.meta.admin && !auth.user?.isAdmin) return { name: 'profile' }
      return true
    }
  } catch {
    return { path: '/login', query: { error: 'auth_unavailable', returnTo: to.fullPath } }
  }
  return { path: '/login', query: { returnTo: to.fullPath } }
})

router.afterEach((to) => {
  document.title = `${String(to.meta.title || '控制台')} - AiFerry`
})

export default router
