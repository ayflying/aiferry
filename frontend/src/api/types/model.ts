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
