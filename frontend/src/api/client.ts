import axios, { type AxiosResponse } from 'axios'
import type { ApiEnvelope } from './types'

const client = axios.create({
  baseURL: '/api/admin',
  timeout: 30_000,
  withCredentials: true,
})

client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (axios.isAxiosError(error) && error.response?.status === 401 && window.location.pathname !== '/login') {
      const returnTo = `${window.location.pathname}${window.location.search}`
      window.location.assign(`/login?returnTo=${encodeURIComponent(returnTo)}`)
    }
    return Promise.reject(error)
  },
)

export async function apiGet<T>(url: string, params?: Record<string, unknown>): Promise<T> {
  return unwrap(client.get<ApiEnvelope<T>>(url, { params }))
}

export async function apiPost<T>(url: string, data?: unknown): Promise<T> {
  return unwrap(client.post<ApiEnvelope<T>>(url, data))
}

export async function apiPut<T>(url: string, data?: unknown): Promise<T> {
  return unwrap(client.put<ApiEnvelope<T>>(url, data))
}

export async function apiDelete<T>(url: string): Promise<T> {
  return unwrap(client.delete<ApiEnvelope<T>>(url))
}

async function unwrap<T>(request: Promise<AxiosResponse<ApiEnvelope<T>>>): Promise<T> {
  try {
    const response = await request
    if (response.data.code !== 0) throw new Error(response.data.message || '请求失败')
    return response.data.data
  } catch (error) {
    if (axios.isAxiosError(error)) {
      throw new Error(error.response?.data?.message || error.message || '网络请求失败')
    }
    throw error
  }
}
