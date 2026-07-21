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
