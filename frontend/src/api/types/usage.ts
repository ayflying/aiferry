export interface Summary {
  requests: number
  successes: number
  inputTokens: number
  outputTokens: number
  totalTokens: number
  estimatedCost?: number
  averageLatency: number
}

export interface TrendPoint {
  bucket: string
  requests: number
  inputTokens: number
  outputTokens: number
  estimatedCost?: number
}

export interface Breakdown {
  name: string
  requests: number
  totalTokens: number
  estimatedCost?: number
}

export interface Dashboard {
  summary: Summary
  trend: TrendPoint[]
  byModel: Breakdown[]
  byChannel: Breakdown[]
}

export interface UsageLog {
  id: number
  requestId: string
  userId: number
  userName: string
  apiKeyName: string
  channelName: string
  endpoint: string
  upstreamEndpoint: string
  protocolConversion: string
  clientIp?: string
  ipLocation?: string
  requestedModel: string
  upstreamModel: string
  httpStatus: number
  isStream: number
  inputTokens?: number
  cachedInputTokens?: number
  outputTokens?: number
  totalTokens?: number
  estimatedCost?: number
  durationMs: number
  firstTokenMs?: number
  attempts: number
  errorMessage: string
  createdAt: string
}

export interface UsagePage {
  items: UsageLog[]
  summary: UsageLogSummary
  startAt: string
  endAt: string
  total: number
  page: number
  pageSize: number
}

export interface UsageLogSummary {
  requests: number
  estimatedCost: number
}
