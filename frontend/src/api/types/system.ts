export interface SensitiveWordSettings {
  imageEnabled: boolean
  enabled: boolean
  checkUserPrompt: boolean
  keywords: string[]
}

export interface SystemInformationSettings {
  systemName: string
  serverUrl: string
  logoUrl: string
  footer: string
  about: string
  homeContent: string
  userAgreement: string
  privacyPolicy: string
  publicHomepageEnabled: boolean
}

export interface ModelQualitySettings {
  enabled: boolean
}

export interface ModelQualityEvent {
  id: number
  channelId: number
  channelName: string
  credentialId: number
  credentialIndex: number
  requestedModel: string
  expectedModel: string
  observedModel: string
  reasons: string[]
  questionChars: number
  answerChars: number
  createdAt: string
}

export interface ModelQualityEventPage {
  items: ModelQualityEvent[]
  total: number
}
