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
  backupBaseUrls: string[]
  forceOpenAIFormat: boolean
  reasoningToContent: boolean
  passthroughRequestBody: boolean
  passthroughPromptCache: boolean
  // promptCacheMode 决定该渠道如何处置请求体里的提示缓存字段：'stable'（缺省）
  // 或空值＝剥离客户端字段并注入网关按用户与凭据生成的稳定缓存键；'off'＝只剥离
  // 客户端字段、不下发任何缓存字段（上游对未知字段严格校验时使用，例如返回
  // UNKNOWN_FIELD 的供应商）；'passthrough'＝完全不干预，由客户端控制。
  // 历史渠道的配置里可能没有这个键，读取时按缺省处理。
  promptCacheMode?: 'stable' | 'off' | 'passthrough' | ''
  skipAsyncPollingDelay: boolean
  systemPrompt: string
  appendSystemPrompt: boolean
  allowServiceTier: boolean
  blockStore: boolean
  allowSafetyIdentifier: boolean
  allowInclude: boolean
  allowInferenceGeo: boolean
  // concurrencyLimit 是每把上游密钥允许同时进行的转发请求数，0 表示不限制。
  // 额度按「渠道 × 密钥」独立计数，多把密钥互不占用。
  concurrencyLimit: number
  // protocolConversion 控制该渠道是否参与 Chat Completions 与 Responses 的
  // 自动协议转换。null 表示跟随系统设置，true/false 为渠道级强制开关。
  protocolConversion: boolean | null
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
  adapter: 'none' | 'openai_costs' | 'sub2api_usage' | 'newapi_balance' | 'custom_json' | 'qiniu_costs' | 'siliconflow_balance' | 'openrouter_credits' | 'qiniu_usage'
  valueType?: 'cost' | 'usage'
  method: string
  path: string
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  headerPrefix: string
  usedPath: string
  remainingPath: string
  currencyPath: string
  fixedCurrency: string
  usagePath?: string
  usageUnit?: string
  usageType?: string
  usageDimension?: string
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

export interface ChannelTypeQuotaConfig {
  adapter: 'none' | 'zhipu_coding_plan' | 'opencode_go_usage' | 'volcengine_afp'
  method: string
  path: string
  authType: 'none' | 'channel_key' | 'management_key'
  headerName: string
  headerPrefix: string
}

export interface ChannelTypeAudioConfig {
  adapter: 'openai' | 'chat'
}

export interface ChannelTypeVideoConfig {
  adapter: 'openai' | 'minimax' | 'volcengine_ark'
}

export interface ChannelTypeProtocolConfig {
  chatCompletionsOnly: boolean
}

export interface ChannelTypeConfig {
  baseUrl: string
  models: ChannelTypeModelConfig
  costs: ChannelTypeCostConfig
  pricing: ChannelTypePricingConfig
  quota?: ChannelTypeQuotaConfig
  audio?: ChannelTypeAudioConfig
  video?: ChannelTypeVideoConfig
  protocol?: ChannelTypeProtocolConfig
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
  costQueryType?: 'cost' | 'usage'
  quotaSupported: boolean
  costQueryConfig: CostQueryConfig
  advancedConfig: ChannelAdvancedConfig
  enabledModelCount: number
  healthyModelCount: number
  disabledModelCount: number
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
  lastCostUsage?: number
  lastCostUsageUnit?: string
  lastCostUsageType?: string
  lastCostUsageDimension?: string
  costSummaries: CostSummary[]
  groupIds: number[]
  createdAt: string
}

export interface CostSummary {
  currency: string
  usedAmount?: number
  remainingAmount?: number
  usage?: number
  usageUnit?: string
  usageType?: string
  usageDimension?: string
}

export interface ChannelCredential {
  id: number
  /** 固定序号：按创建顺序编号（含已删密钥占位），与用量明细「渠道 #N」一致；删除不重排。 */
  index: number
  keyPrefix: string
  hasManagementKey?: boolean
  status: number
  autoDisabled: boolean
  autoDisabledAt?: string
  autoDisabledReason: string
  autoDisabledStatusCode?: number
  lastCostUsed?: number
  lastCostRemaining?: number
  lastCostCurrency: string
  lastCostAt?: string
  lastCostUsage?: number
  lastCostUsageUnit?: string
  lastCostUsageType?: string
  lastCostUsageDimension?: string
  createdAt: string
}

/** 查看上游密钥明文的邮箱验证状态（10 分钟窗口）。 */
export interface CredentialRevealStatus {
  /** true＝验证窗口仍有效，可直接揭示密钥。 */
  verified: boolean
  /** false＝个人资料未填邮箱，无法发送验证码。 */
  emailReady: boolean
  /** 脱敏邮箱，如 a***@example.com。 */
  emailMasked: string
  /** 验证窗口剩余秒数（仅 verified 时有意义）。 */
  expiresInSeconds?: number
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
  usage?: number
  usageUnit?: string
  usageType?: string
  usageDimension?: string
}

export interface ChannelCostResult {
  mode: string
  usedAmount?: number
  remainingAmount?: number
  currency: string
  queriedAt: string
  usage?: number
  usageUnit?: string
  usageType?: string
  usageDimension?: string
  summaries: CostSummary[]
  credentials: ChannelCredentialCost[]
}

export interface ChannelQuotaWindow {
  kind: 'five_hour' | 'weekly' | 'monthly' | 'mcp'
  label: string
  usedPercent: number
  used?: number
  total?: number
  remaining?: number
  nextResetAt?: string
}

export interface ChannelQuotaResult {
  mode: string
  level: string
  windows: ChannelQuotaWindow[]
  queriedAt: string
  cached: boolean
  /** 多密钥合并查询中失败密钥的明细；为空表示全部成功 */
  partialErrors?: string[]
}

export interface SystemResilienceSettings {
  retryStatusCodes: string
  streamFirstByteTimeoutSeconds: number
  streamIdleTimeoutSeconds: number
  nonStreamTimeoutSeconds: number
  healthCheckEnabled: boolean
  healthCheckMode: 'passive' | 'all'
  healthCheckIntervalMinutes: number
	recoveryEnabled: boolean
	autoDisableEnabled: boolean
	autoDisableNotificationEnabled: boolean
	autoDisableFailureThreshold: number
  disableLatencySeconds: number
  disableStatusCodes: string
  failureKeywords: string[]
  modelQualityDetectionEnabled: boolean
  // protocolConversionEnabled 控制网关是否允许在 Chat Completions 与 Responses
  // 之间自动转换；关闭后请求直连客户端声明的端点，用于排查转换引入的问题。
  protocolConversionEnabled: boolean
  // streamFailureEventEnabled 控制流式响应在已经写出内容后失败时，是否补发
  // 显式错误事件与结束帧；关闭后保持直接断开流的历史行为。
  streamFailureEventEnabled: boolean
  // messagesModels 是全局 Anthropic Messages 模型名单（前缀-* 通配或精确名）；
  // 渠道启用协议转换后命中即自动转 messages，无需改渠道类型。空名单不启用。
  messagesModels: string[]
  // messagesPath 是全局 messages 端点：相对路径拼渠道 baseUrl，完整 URL 直接
  // 使用（如 Zen 的跨源 /zen/v1/messages）；留空用协议缺省 /v1/messages。
  messagesPath: string
}

export interface BaseSettings {
  timeZone: string
  // displayCurrency 是金额展示货币；只影响展示，不改写库内结算金额与结算币种。
  displayCurrency: DisplayCurrency
  // exchangeRateMode 决定汇率来源：auto 取公开汇率接口，manual 只用人工填写的汇率。
  exchangeRateMode: ExchangeRateMode
  // manualUsdToCnyRate 是人工 USD→CNY 汇率，也是自动汇率不可用时的兜底值。
  manualUsdToCnyRate: number
}

export type DisplayCurrency = 'USD' | 'CNY'

export type ExchangeRateMode = 'auto' | 'manual'

// CurrencyRateSettings 是当前生效的折算汇率与其来源，管理端据此核对自动汇率是否取到。
export interface CurrencyRateSettings {
  display: DisplayCurrency
  base: string
  // rates 的语义是「1 个 base 能换多少该币种」。
  rates: Record<string, number>
  // source 取值 auto/manual/fallback：fallback 表示自动汇率取不到、正在用人工兜底值。
  source: 'auto' | 'manual' | 'fallback'
  updatedAt?: string
}

export interface MailSettings {
  enabled: boolean
  channelAlertEnabled: boolean
  channelBalanceThresholds: string
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

// 代理连通性测试结果：业务失败（代理不通）也走 ok=false 回传，不是接口错误。
export interface ProxyTestResult {
  ok: boolean
  latencyMs: number
  message: string
}
