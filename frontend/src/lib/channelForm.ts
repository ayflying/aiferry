import type { ChannelAdvancedConfig, ChannelInput } from '../api/types'

export function createDefaultChannelAdvancedConfig(): ChannelAdvancedConfig {
  return {
    forceOpenAIFormat: false,
    reasoningToContent: false,
    enableProtocolConversion: true,
    passthroughRequestBody: false,
    skipAsyncPollingDelay: false,
    systemPrompt: '',
    appendSystemPrompt: false,
    allowServiceTier: false,
    blockStore: true,
    allowSafetyIdentifier: false,
    allowInclude: false,
    allowInferenceGeo: false,
  }
}

export function createEmptyChannelInput(): ChannelInput {
  return {
    name: '', type: '', baseUrl: 'https://api.openai.com/v1', apiKey: '', managementKey: '', proxyUrl: '',
    organizationId: '', projectId: '', status: 1, priority: 0, weight: 1, healthCheckModelId: 0,
    autoDisableEnabled: true, advancedConfig: createDefaultChannelAdvancedConfig(), groupIds: [],
  }
}
