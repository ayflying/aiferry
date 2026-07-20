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

export interface HourlyCostPoint {
  bucket: string
  estimatedCost: number
}

export interface RecentCostModel {
  name: string
  points: HourlyCostPoint[]
}

export interface RecentCostDistribution {
  totalEstimatedCost: number
  models: RecentCostModel[]
}

export interface Dashboard {
  summary: Summary
  trend: TrendPoint[]
  byModel: Breakdown[]
  byChannel: Breakdown[]
  recentCost: RecentCostDistribution
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
  reasoningEffort: string
  httpStatus: number
  isStream: number
  inputTokens?: number
  cachedInputTokens?: number
  outputTokens?: number
  totalTokens?: number
  estimatedCost?: number
  billingDetails?: BillingDetails
  durationMs: number
  firstTokenMs?: number
  attempts: number
  errorMessage: string
  createdAt: string
}

export interface BillingItem {
  type: 'input' | 'cached_input' | 'cache_write' | 'output' | 'image_input' | 'audio_input' | 'audio_output' | 'request' | 'rounding'
  quantity: number
  unit: 'per_million_tokens' | 'per_request' | 'settlement'
  unitPrice: string
  priceSource?: string
  amount: string
}

export interface BillingRuleSnapshot {
  id: number
  name: string
  source: string
  priority: number
  conditions: string
}

export interface BillingDetails {
  billingMode: 'token' | 'request' | 'rules'
  currency: string
  charged: boolean
  reconstructed?: boolean
  rule?: BillingRuleSnapshot
  items: BillingItem[]
  subtotal: string
  total: string
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
