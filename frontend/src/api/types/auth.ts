import type { SystemInformationSettings } from './system'

export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
}

export interface AuthConfig {
  enabled: boolean
  provider: string
  loginPath: string
  timeZone: string
  currency: AuthCurrency
  system: SystemInformationSettings
}

// AuthCurrency 是服务端下发的展示货币与折算汇率。
// 展示货币与汇率都由服务端集中管理（含 Redis 缓存与人工兜底），
// 前端只用同一份汇率做展示折算，保证各页面口径一致。
export interface AuthCurrency {
  display: string
  base: string
  // rates 的语义是「1 个 base 能换多少该币种」。
  rates: Record<string, number>
  source?: string
  updatedAt?: string
}

export interface AuthUser {
  id: number
  name: string
  role: string
  isAdmin: boolean
  avatarUrl: string
}

export interface AccountUsageSummary {
  days: number
  requests: number
  successes: number
  inputTokens: number
  outputTokens: number
  totalTokens: number
  estimatedCost: number
}

export interface AccountProfile {
  id: number
  nickname: string
  email: string
  role: string
  balance: number
  avatarUrl: string
  createdAt: string
  lastLoginAt?: string
}

export interface ManagedUser extends AccountProfile {
  apiKeyCount: number
  usage: AccountUsageSummary
}
