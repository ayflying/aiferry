import axios from 'axios'
import type { ApiEnvelope, AuthConfig, AuthUser } from './types'

export const authClient = axios.create({
  baseURL: '/api/auth',
  timeout: 15_000,
  withCredentials: true,
})

export async function loadAuthConfig(): Promise<AuthConfig> {
  const response = await authClient.get<ApiEnvelope<AuthConfig>>('/config')
  if (response.data.code !== 0) throw new Error(response.data.message || '无法读取登录配置')
  return response.data.data
}

export async function loadCurrentUser(): Promise<AuthUser | null> {
  try {
    const response = await authClient.get<ApiEnvelope<AuthUser>>('/me')
    if (response.data.code !== 0) throw new Error(response.data.message || '无法读取登录状态')
    return response.data.data
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.status === 401) return null
    throw error
  }
}

export async function logout(): Promise<void> {
  const response = await authClient.post<ApiEnvelope<Record<string, never>>>('/logout')
  if (response.data.code !== 0) throw new Error(response.data.message || '退出登录失败')
}
