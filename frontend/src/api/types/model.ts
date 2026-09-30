export type ModelBillingMode = 'token' | 'request' | 'rules'

export type ModelModality = 'text' | 'image' | 'audio' | 'video' | 'file'

/** null 表示未知；用于 manual 时回退 automatic。空模态列表表示明确无。 */
export interface ModelMetadata {
  display_name: string | null
  description: string | null
  input_modalities: ModelModality[] | null
  output_modalities: ModelModality[] | null
  context_length: number | null
  max_output_tokens: number | null
  capabilities: {
    tools: boolean | null
    reasoning: boolean | null
    structured_output: boolean | null
  }
}

export interface ModelMetadataResponse {
  automatic: ModelMetadata
  manual: ModelMetadata | null
  effective: ModelMetadata
}

/**
 * 时间窗：时区 + 星期（ISO 1=周一 … 7=周日，空表示每天）+ 一天内的多个时段。
 * 时段元素为 [开始, 结束]，起止相同表示全天，开始晚于结束表示跨零点。
 */
export interface TimeWindow {
  tz?: string
  weekdays?: number[]
  ranges?: Array<[string, string]>
}

export interface ChannelModel {
  id: number
  channelId: number
  channelName: string
  publicName: string
  upstreamName: string
  discovered: number
  enabled: number
  healthScore: number
  autoDisabled: boolean
  autoDisabledAt?: string
  autoDisabledReason: string
  /** 定时关闭时段列表（任一窗口命中即关闭）；未配置时为 null 或空 */
  closedWindows?: TimeWindow[] | null
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
  /**
   * 密钥组合健康（只读展示）。存在时，列表的健康分取其中有效密钥的最高分；
   * 可展开/悬浮查看每把 key 的组合分与隔离到期。不暴露密钥密文/明文。
   */
  credentialHealth?: CredentialHealth[]
}

/** 单个「渠道 × 模型 × 密钥」组合的健康只读信息 */
export interface CredentialHealth {
  /** 渠道密钥 ID */
  credentialId: number
  /** 密钥前缀（展示用，非密钥本身） */
  keyPrefix: string
  /** 组合健康分（0-100） */
  healthScore: number
  /** 隔离/冷却截至时间；未隔离为 null */
  cooldownUntil?: string | null
  /** 最近一次错误摘要（不泄露密钥） */
  lastError?: string
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
  publicName: string
  selected: boolean
  /** 手动添加的自定义模型（上游发现结果中没有，取消勾选即移除） */
  custom?: boolean
}

export interface ModelTestResult {
  success: boolean
  endpoint: 'chat' | 'responses' | 'embeddings' | 'images' | 'tts' | 'asr'
  stream: boolean
  model: string
  latencyMs: number
  httpStatus: number
  inputTokens: number
  outputTokens: number
  message: string
}
