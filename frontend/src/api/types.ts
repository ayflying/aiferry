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
  isAdmin: boolean
  avatarUrl: string
	groups: string[]
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

export interface CostQueryConfig {
  url: string
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  usedPath: string
  remainingPath: string
  currencyPath: string
  fixedCurrency: string
}

export interface ChannelAdvancedConfig {
  forceOpenAIFormat: boolean
  reasoningToContent: boolean
  passthroughRequestBody: boolean
  skipAsyncPollingDelay: boolean
  systemPrompt: string
  appendSystemPrompt: boolean
  allowServiceTier: boolean
  blockStore: boolean
  allowSafetyIdentifier: boolean
  allowInclude: boolean
  allowInferenceGeo: boolean
}

export interface ChannelTypeModelConfig {
  method: string
  path: string
  listPath: string
  idPath: string
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  headerPrefix: string
}

export interface ChannelTypeCostConfig {
  adapter: 'none' | 'openai_costs' | 'sub2api_usage' | 'custom_json'
  method: string
  path: string
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  headerPrefix: string
  usedPath: string
  remainingPath: string
  currencyPath: string
  fixedCurrency: string
}

export interface ChannelTypePricingConfig {
  adapter: 'none' | 'json'
  method: string
  path: string
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  headerPrefix: string
  listPath: string
  modelPath: string
  namePath: string
  currencyPath: string
  conditionsPath: string
  ratesPath: string
  inputPricePath: string
  cachedInputPricePath: string
  cacheWritePricePath: string
  outputPricePath: string
  imageInputPricePath: string
  audioInputPricePath: string
  audioOutputPricePath: string
  requestPricePath: string
}

export interface ChannelTypeEndpointConfig {
  method: 'GET' | 'POST' | 'DELETE'
  path: string
  requestBody: 'json' | 'multipart' | 'none'
  supportsStream: boolean
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  headerPrefix: string
}

export interface ChannelTypeConfig {
  models: ChannelTypeModelConfig
  costs: ChannelTypeCostConfig
  pricing: ChannelTypePricingConfig
  endpoints: Record<string, ChannelTypeEndpointConfig>
}

export interface PriceSourceConfig {
  baseUrl: string
  pricing: Omit<ChannelTypePricingConfig, 'adapter' | 'authType'> & {
    adapter: 'newapi_ratio' | 'json'
    authType: 'none'
  }
}

export interface PriceSource {
  id: number
  name: string
  code: string
  config: PriceSourceConfig
  status: number
  builtIn: number
  createdAt: string
  updatedAt: string
}

export interface ChannelType {
  id: number
  name: string
  code: string
  config: ChannelTypeConfig
  status: number
  builtIn: number
  createdAt: string
  updatedAt: string
}

export interface Channel {
  id: number
  name: string
  type: string
  typeName: string
  baseUrl: string
  hasApiKey: boolean
  hasManagementKey: boolean
	 hasProxy: boolean
  organizationId: string
  projectId: string
  status: number
  autoDisabled: boolean
  autoDisabledAt?: string
  autoDisabledReason: string
  autoDisabledStatusCode?: number
  priority: number
  weight: number
  healthCheckModelId: number
  autoDisableEnabled: boolean
  costQueryMode: string
  costQueryConfig: CostQueryConfig
	 advancedConfig: ChannelAdvancedConfig
  enabledModelCount: number
  discoveredModels: number
  credentialCount: number
  activeCredentialCount: number
  credentialsUnavailable: boolean
  lastTestStatus: string
  lastTestLatencyMs: number
  lastTestError: string
  lastTestAt?: string
  lastCostUsed?: number
  lastCostRemaining?: number
  lastCostCurrency: string
  lastCostAt?: string
  costSummaries: CostSummary[]
  groupIds: number[]
  createdAt: string
}

export interface CostSummary {
  currency: string
  usedAmount?: number
  remainingAmount?: number
}

export interface ChannelCredential {
  id: number
  keyPrefix: string
  status: number
  autoDisabled: boolean
  autoDisabledAt?: string
  autoDisabledReason: string
  autoDisabledStatusCode?: number
  lastCostUsed?: number
  lastCostRemaining?: number
  lastCostCurrency: string
  lastCostAt?: string
  createdAt: string
}

export interface ChannelCredentialCost {
  credentialId: number
  keyPrefix: string
  shared: boolean
  usedAmount?: number
  remainingAmount?: number
  currency: string
  queriedAt: string
  error: string
}

export interface ChannelCostResult {
  mode: string
  usedAmount?: number
  remainingAmount?: number
  currency: string
  queriedAt: string
  summaries: CostSummary[]
  credentials: ChannelCredentialCost[]
}

export interface SystemResilienceSettings {
  maxFailoverAttempts: number
  retryStatusCodes: string
  healthCheckEnabled: boolean
  healthCheckMode: 'passive' | 'all'
  healthCheckIntervalMinutes: number
  recoveryEnabled: boolean
  autoDisableEnabled: boolean
  disableLatencySeconds: number
  disableStatusCodes: string
  failureKeywords: string[]
}

export interface MailSettings {
  enabled: boolean
  host: string
  port: number
  username: string
  passwordConfigured: boolean
  from: string
  security: 'none' | 'starttls' | 'tls'
  threshold: number
  subjectTemplate: string
  bodyTemplate: string
}

export interface ChannelInput {
  name: string
  type: string
  baseUrl: string
  apiKey?: string
  managementKey?: string
  proxyUrl?: string
  organizationId: string
  projectId: string
  status: number
  priority: number
  weight: number
  healthCheckModelId: number
  autoDisableEnabled: boolean
  advancedConfig: ChannelAdvancedConfig
  groupIds: number[]
}

export type ModelBillingMode = 'token' | 'request' | 'rules'

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
  cacheWritePrice?: number
  outputPrice?: number
  imageInputPrice?: number
  audioInputPrice?: number
  audioOutputPrice?: number
  requestPrice?: number
  billingMode: ModelBillingMode
  lastTestEndpoint: string
  lastTestStatus: string
  lastTestLatencyMs: number
  lastTestError: string
  lastTestAt?: string
  updatedAt: string
}

export interface PublicModel {
  id: number
  publicName: string
  inputPrice?: number
  cachedInputPrice?: number
  cacheWritePrice?: number
  outputPrice?: number
  imageInputPrice?: number
  audioInputPrice?: number
  audioOutputPrice?: number
  requestPrice?: number
  billingMode: ModelBillingMode
}

export interface DiscoveredModel {
  name: string
  selected: boolean
}

export interface ModelTestResult {
  success: boolean
  endpoint: 'chat' | 'responses' | 'embeddings' | 'images'
  stream: boolean
  model: string
  latencyMs: number
  httpStatus: number
  inputTokens: number
  outputTokens: number
  message: string
}

export interface CreatedAPIKey extends APIKey {
  key: string
}

export interface ChannelGroup {
  id: number
  name: string
  code: string
  description: string
  status: number
  channelIds: number[]
  createdAt: string
  updatedAt: string
}

export interface APIKey {
  id: number
  userId: number
  name: string
  keyPrefix: string
  secretAvailable: boolean
  status: number
  spendLimit?: number
  spentAmount: number
  availableAmount?: number
  allowedModels: string[]
  channelGroupIds: number[]
  expiresAt?: string
  lastUsedAt?: string
  createdAt: string
}

export interface PriceRule {
  id: number
  channelModelId: number
  name: string
  source: 'manual' | 'sync'
  sourceRef: string
  priority: number
  currency: string
  conditions: Record<string, unknown>
  rates: Record<string, number>
  status: number
  syncedAt?: string
  updatedAt: string
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
  userId: number
  userName: string
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
