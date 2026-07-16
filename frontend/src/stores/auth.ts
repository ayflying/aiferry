import { defineStore } from 'pinia'
import { ref } from 'vue'
import { loadCurrentUser, logout as requestLogout } from '../api/auth'
import type { AuthUser } from '../api/types'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<AuthUser | null>(null)
  const loaded = ref(false)
  let pending: Promise<boolean> | null = null

  async function ensureUser(force = false): Promise<boolean> {
    if (loaded.value && !force) return user.value !== null
    if (pending) return pending
    pending = loadCurrentUser()
      .then((current) => {
        user.value = current
        loaded.value = true
        return current !== null
      })
      .finally(() => {
        pending = null
      })
    return pending
  }

  async function logout(): Promise<void> {
    await requestLogout()
    user.value = null
    loaded.value = true
  }

  return { user, loaded, ensureUser, logout }
})
