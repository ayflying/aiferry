import axios, { type AxiosResponse } from 'axios'
import type { AccountProfile, AccountUsageSummary, ApiEnvelope, AuthConfig, AuthUser } from './types'

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

export async function loadProfile(): Promise<AccountProfile> {
  return unwrap(authClient.get<ApiEnvelope<AccountProfile>>('/profile'))
}

export async function updateProfile(input: Pick<AccountProfile, 'nickname' | 'email'>): Promise<AccountProfile> {
  return unwrap(authClient.put<ApiEnvelope<AccountProfile>>('/profile', input))
}

export async function loadPersonalUsage(days = 30): Promise<AccountUsageSummary> {
  return unwrap(authClient.get<ApiEnvelope<AccountUsageSummary>>('/usage', { params: { days } }))
}

async function unwrap<T>(request: Promise<AxiosResponse<ApiEnvelope<T>>>): Promise<T> {
  try {
    const response = await request
    if (response.data.code !== 0) throw new Error(response.data.message || '请求失败')
    return response.data.data
  } catch (error) {
    if (axios.isAxiosError(error)) throw new Error(error.response?.data?.message || error.message || '网络请求失败')
    throw error
  }
}
