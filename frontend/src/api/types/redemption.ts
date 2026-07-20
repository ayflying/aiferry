export type RedemptionCodeStatus = 'active' | 'used' | 'expired'

export interface RedemptionCode {
  id: number
  name: string
  code: string
  amount: number
  status: RedemptionCodeStatus
  expiresAt?: string | null
  redeemedByName?: string
  redeemedAt?: string | null
  createdAt: string
}

export interface CreatedRedemptionCode {
  id: number
  code: string
  amount: number
  expiresAt?: string | null
}

export interface RedemptionResult {
  code: string
  amount: number
}
