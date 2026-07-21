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
  baseUrl: string
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
  streamFirstByteTimeoutSeconds: number
  streamIdleTimeoutSeconds: number
  nonStreamTimeoutSeconds: number
  healthCheckEnabled: boolean
  healthCheckMode: 'passive' | 'all'
  healthCheckIntervalMinutes: number
  recoveryEnabled: boolean
  autoDisableEnabled: boolean
  autoDisableFailureThreshold: number
  disableLatencySeconds: number
  disableStatusCodes: string
  failureKeywords: string[]
}

export interface BaseSettings {
  timeZone: string
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
