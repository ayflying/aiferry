export interface SensitiveWordSettings {
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
}
