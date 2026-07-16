export interface ApiEnvelope<T> {
  code: number
  message: string
  data: T
}

export interface AuthConfig {
	enabled: boolean
	provider: string
	loginPath: string
}

export interface AuthUser {
	id: number
	name: string
	role: string
	avatarUrl: string
	groups: string[]
}

export interface CostQueryConfig {
  url: string
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  usedPath: string
  remainingPath: string
  currencyPath: string
  fixedCurrency: string
}

export interface Channel {
  id: number
  name: string
  type: string
  baseUrl: string
  hasApiKey: boolean
  hasManagementKey: boolean
  organizationId: string
  projectId: string
  status: number
  priority: number
  weight: number
  costQueryMode: string
  costQueryConfig: CostQueryConfig
  enabledModelCount: number
  discoveredModels: number
  lastTestStatus: string
  lastTestLatencyMs: number
  lastTestError: string
  lastTestAt?: string
  lastCostUsed?: number
  lastCostRemaining?: number
  lastCostCurrency: string
  lastCostAt?: string
  createdAt: string
}

export interface ChannelInput {
  name: string
  baseUrl: string
  apiKey?: string
  managementKey?: string
  organizationId: string
  projectId: string
  status: number
  priority: number
  weight: number
  costQueryMode: string
  costQueryConfig: CostQueryConfig
}

export interface ChannelModel {
  id: number
  channelId: number
  channelName: string
  publicName: string
  upstreamName: string
  discovered: number
  enabled: number
  inputPrice?: number
  cachedInputPrice?: number
  outputPrice?: number
  lastTestEndpoint: string
  lastTestStatus: string
  lastTestLatencyMs: number
  lastTestError: string
  lastTestAt?: string
  updatedAt: string
}

export interface DiscoveredModel {
  name: string
  selected: boolean
}

export interface ModelTestResult {
  success: boolean
  endpoint: 'chat' | 'responses' | 'embeddings'
  model: string
  latencyMs: number
  httpStatus: number
  inputTokens: number
  outputTokens: number
  message: string
}

export interface APIKey {
  id: number
  userId: number
  name: string
  keyPrefix: string
  status: number
  expiresAt?: string
  lastUsedAt?: string
  createdAt: string
}

export interface CreatedAPIKey extends APIKey {
  key: string
}

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
  apiKeyName: string
  channelName: string
  endpoint: string
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
  total: number
  page: number
  pageSize: number
}
