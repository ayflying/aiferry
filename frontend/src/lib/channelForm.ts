import type { ChannelAdvancedConfig, ChannelInput, ChannelType } from '../api/types'

export function createDefaultChannelAdvancedConfig(): ChannelAdvancedConfig {
  return {
    backupBaseUrls: [],
    forceOpenAIFormat: false,
    reasoningToContent: false,
    passthroughRequestBody: false,
    passthroughPromptCache: false,
    skipAsyncPollingDelay: false,
    systemPrompt: '',
    appendSystemPrompt: false,
    allowServiceTier: false,
    blockStore: true,
    allowSafetyIdentifier: false,
    allowInclude: false,
    allowInferenceGeo: false,
    concurrencyLimit: 0,
    protocolConversion: null,
  }
}

export function createEmptyChannelInput(): ChannelInput {
  return {
    name: '', type: '', baseUrl: 'https://api.openai.com/v1', apiKey: '', managementKey: '', proxyUrl: '',
    organizationId: '', projectId: '', status: 1, priority: 0, weight: 1, healthCheckModelId: 0,
    autoDisableEnabled: true, advancedConfig: createDefaultChannelAdvancedConfig(), groupIds: [],
  }
}

export function channelTypeBaseURL(types: ChannelType[], code: string): string {
  return types.find((item) => item.code === code)?.config.baseUrl || ''
}

/**
 * 「组织 ID / 项目 ID」是 OpenAI 官方渠道专有的身份标识：转发时作为
 * OpenAI-Organization / OpenAI-Project 请求头注入上游，成本查询按 project_ids 过滤。
 * openai_costs 成本适配器全库只由 OpenAI 官方声明，语义与这两条用途完全重合，
 * 因此用它作判据；其余渠道类型填了上游也不认，表单不展示这两个字段。
 */
export function supportsOrganizationIdentity(type: ChannelType | undefined): boolean {
  return type?.config?.costs?.adapter === 'openai_costs'
}
