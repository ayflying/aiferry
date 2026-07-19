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
