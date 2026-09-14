// 渠道类型 config 的结构化编辑逻辑。
//
// 关键约定：config 是一棵由后端 `channeltype.Config` 定义的 JSON（解析时
// DisallowUnknownFields，字段名写错直接报错）。因此表单必须做到「无损」——
// 以解析出来的对象为草稿，表单只覆盖它渲染的字段，未渲染的键原样保留，
// 这样后端将来新增字段时，旧版前端提交也不会把它们悄悄抹掉。

export type TypeConfigRecord = Record<string, any>

export interface ConfigOption {
  value: string
  label: string
}

/** 各分组的默认骨架：后端对应字段缺失时用它撑起表单，避免模板访问 undefined。 */
const SECTION_SKELETONS: Record<string, () => TypeConfigRecord> = {
  models: () => ({ method: 'GET', path: '', listPath: '', idPath: '', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ' }),
  costs: () => ({ adapter: 'none', valueType: 'cost', method: 'GET', path: '', authType: 'management_key', headerName: 'Authorization', headerPrefix: 'Bearer ' }),
  pricing: () => ({ adapter: 'none', method: 'GET', path: '', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ' }),
  quota: () => ({ adapter: 'none', method: 'GET', path: '', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ' }),
  audio: () => ({ adapter: 'openai' }),
  video: () => ({ adapter: 'openai' }),
  protocol: () => ({ chatCompletionsOnly: false }),
}

/** 可以在表单里维护的分组（endpoints 结构特殊，另行处理）。 */
export const FORM_SECTIONS = ['models', 'costs', 'pricing', 'quota', 'audio', 'video', 'protocol'] as const

export const AUTH_TYPE_OPTIONS: ConfigOption[] = [
  { value: 'channel_key', label: '渠道推理密钥' },
  { value: 'management_key', label: '渠道管理密钥' },
  { value: 'none', label: '无需鉴权' },
]

export const COST_ADAPTER_OPTIONS: ConfigOption[] = [
  { value: 'none', label: '不查询费用' },
  { value: 'openai_costs', label: 'OpenAI 成本接口' },
  { value: 'sub2api_usage', label: 'Sub2API 用量' },
  { value: 'newapi_balance', label: 'New API 余额' },
  { value: 'custom_json', label: '自定义 JSON 字段' },
  { value: 'qiniu_costs', label: '七牛云成本' },
  { value: 'qiniu_usage', label: '七牛云用量（旧版）' },
  { value: 'siliconflow_balance', label: '硅基流动余额' },
  { value: 'openrouter_credits', label: 'OpenRouter 余额' },
]

export const VALUE_TYPE_OPTIONS: ConfigOption[] = [
  { value: 'cost', label: '余额 / 成本' },
  { value: 'usage', label: '用量额度' },
]

export const PRICING_ADAPTER_OPTIONS: ConfigOption[] = [
  { value: 'none', label: '不同步价格' },
  { value: 'json', label: 'JSON 接口同步' },
]

export const QUOTA_ADAPTER_OPTIONS: ConfigOption[] = [
  { value: 'none', label: '不支持' },
  { value: 'zhipu_coding_plan', label: '智谱 Coding Plan' },
  { value: 'opencode_go_usage', label: 'OpenCode Go 用量' },
  { value: 'volcengine_afp', label: '火山方舟 AFP' },
]

export const AUDIO_ADAPTER_OPTIONS: ConfigOption[] = [
  { value: 'openai', label: '标准 OpenAI 音频端点' },
  { value: 'chat', label: '经由 chat completions 承载' },
]

export const VIDEO_ADAPTER_OPTIONS: ConfigOption[] = [
  { value: 'openai', label: '标准 OpenAI 视频端点' },
  { value: 'minimax', label: 'MiniMax 视频协议' },
  { value: 'volcengine_ark', label: '火山方舟内容生成任务' },
]

export const METHOD_OPTIONS: ConfigOption[] = [
  { value: 'GET', label: 'GET' },
  { value: 'POST', label: 'POST' },
  { value: 'DELETE', label: 'DELETE' },
]

export const REQUEST_BODY_OPTIONS: ConfigOption[] = [
  { value: 'json', label: 'JSON' },
  { value: 'multipart', label: 'multipart' },
  { value: 'none', label: '无请求体' },
]

/** 价格同步里与具体数值相关的路径字段，adapter 为 json 时才需要填写。 */
export const PRICING_PATH_FIELDS: Array<{ key: string; label: string; hint?: string }> = [
  { key: 'listPath', label: '价格表所在路径', hint: '响应中承载全部模型的节点，留空表示根节点' },
  { key: 'modelPath', label: '模型名字段', hint: '模型列表为数组时需要' },
  { key: 'namePath', label: '模型名称字段' },
  { key: 'currencyPath', label: '货币字段' },
  { key: 'conditionsPath', label: '条件字段' },
  { key: 'ratesPath', label: '倍率对象字段', hint: '填写后按倍率换算，无需逐项配置下面的价格路径' },
  { key: 'inputPricePath', label: '输入价格字段' },
  { key: 'cachedInputPricePath', label: '缓存命中价格字段' },
  { key: 'cacheWritePricePath', label: '缓存写入价格字段' },
  { key: 'outputPricePath', label: '输出价格字段' },
  { key: 'imageInputPricePath', label: '图像输入价格字段' },
  { key: 'audioInputPricePath', label: '音频输入价格字段' },
  { key: 'audioOutputPricePath', label: '音频输出价格字段' },
  { key: 'requestPricePath', label: '按次计费价格字段' },
]

/** 常用端点名，仅作为新增端点时的输入建议。 */
export const ENDPOINT_NAME_SUGGESTIONS = [
  'chatCompletions', 'responses', 'embeddings', 'imagesGenerations', 'imagesEdits',
  'audioSpeech', 'audioTranscriptions', 'audioTranslations', 'videoGenerations',
  'videoRetrieve', 'videoContent', 'videoRemix', 'moderations', 'files', 'batches',
  'fineTuningJobs', 'realtimeSessions', 'realtimeClientSecrets',
]

export function parseTypeConfigText(text: string): { config: TypeConfigRecord | null; error: string } {
  const trimmed = text.trim()
  if (!trimmed) return { config: null, error: '' }
  try {
    const parsed = JSON.parse(trimmed)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { config: null, error: '配置必须是一个 JSON 对象' }
    }
    return { config: parsed as TypeConfigRecord, error: '' }
  } catch (error) {
    return { config: null, error: error instanceof Error ? error.message : 'JSON 解析失败' }
  }
}

export function formatTypeConfig(config: TypeConfigRecord): string {
  return JSON.stringify(config, null, 2)
}

/**
 * 把解析结果转成表单草稿：补齐缺失的分组骨架，其余键（含未知键）原样保留。
 * 刻意不补 `endpoints`：后端 `endpoints` 缺失时回落到内置端点表，补成空对象反而会
 * 触发「endpoints must not be empty」校验。
 */
export function configForForm(config: TypeConfigRecord | null): TypeConfigRecord {
  const source = config ? JSON.parse(JSON.stringify(config)) as TypeConfigRecord : {}
  for (const section of FORM_SECTIONS) {
    const current = source[section]
    if (current && typeof current === 'object' && !Array.isArray(current)) continue
    source[section] = SECTION_SKELETONS[section]()
  }
  if (typeof source.baseUrl !== 'string') source.baseUrl = ''
  return source
}

/** 是否展示余额/成本相关路径（adapter 为 none 时这些字段无意义）。 */
export function costsShowsBalanceFields(config: TypeConfigRecord): boolean {
  const adapter = String(config?.costs?.adapter ?? 'none')
  return adapter !== 'none' && !costsShowsUsageFields(config)
}

/** 是否按「用量」语义展示字段，与后端 `channeltype.IsUsageCost` 保持一致。 */
export function costsShowsUsageFields(config: TypeConfigRecord): boolean {
  const costs = config?.costs
  if (!costs) return false
  return String(costs.valueType ?? '') === 'usage' || String(costs.adapter ?? '') === 'qiniu_usage'
}

/** 价格同步的路径字段仅在 adapter 为 json 时可见。 */
export function pricingDetailVisible(config: TypeConfigRecord): boolean {
  return String(config?.pricing?.adapter ?? 'none') === 'json'
}

/**
 * 部分套餐额度适配器的请求方式与鉴权是固定的（后端会强制覆盖），
 * 表单里对应字段只读即可，避免给出「填了会生效」的错觉。
 */
export function quotaDetailLocked(config: TypeConfigRecord): boolean {
  const adapter = String(config?.quota?.adapter ?? 'none')
  return adapter === 'opencode_go_usage' || adapter === 'volcengine_afp'
}

/** 套餐额度是否需要填写路径与鉴权（仅智谱适配器会真正用到）。 */
export function quotaDetailVisible(config: TypeConfigRecord): boolean {
  return String(config?.quota?.adapter ?? 'none') !== 'none'
}

export function endpointNames(config: TypeConfigRecord): string[] {
  const endpoints = config?.endpoints
  if (!endpoints || typeof endpoints !== 'object' || Array.isArray(endpoints)) return []
  return Object.keys(endpoints)
}

export function createEndpointConfig(): TypeConfigRecord {
  return {
    method: 'POST', path: '', requestBody: 'json', supportsStream: false,
    authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ',
  }
}

/** 端点名称是对象的键，重命名时要在保持插入顺序的前提下换键。 */
export function renameEndpoint(config: TypeConfigRecord, from: string, to: string): boolean {
  const endpoints = config?.endpoints
  if (!endpoints || typeof endpoints !== 'object' || Array.isArray(endpoints)) return false
  const name = to.trim()
  if (!name || name === from) return false
  if (Object.prototype.hasOwnProperty.call(endpoints, name)) return false
  const rebuilt: TypeConfigRecord = {}
  for (const key of Object.keys(endpoints)) {
    rebuilt[key === from ? name : key] = endpoints[key]
  }
  config.endpoints = rebuilt
  return true
}

export function modelsSummary(config: TypeConfigRecord): string {
  const models = config?.models ?? {}
  const method = String(models.method || 'GET')
  return `${method} ${String(models.path || '未填写路径')}`
}

export function costsSummary(config: TypeConfigRecord): string {
  const costs = config?.costs ?? {}
  const adapter = String(costs.adapter || 'none')
  if (adapter === 'none') return '未启用'
  const label = COST_ADAPTER_OPTIONS.find((item) => item.value === adapter)?.label ?? adapter
  return `${label} · ${costsShowsUsageFields(config) ? '用量' : '余额'}`
}

export function pricingSummary(config: TypeConfigRecord): string {
  return pricingDetailVisible(config) ? `JSON 接口 · ${String(config?.pricing?.path || '未填写路径')}` : '未启用'
}

export function quotaSummary(config: TypeConfigRecord): string {
  const adapter = String(config?.quota?.adapter || 'none')
  if (adapter === 'none') return '不支持'
  return QUOTA_ADAPTER_OPTIONS.find((item) => item.value === adapter)?.label ?? adapter
}

export function endpointSummary(config: TypeConfigRecord): string {
  const count = endpointNames(config).length
  return count ? `${count} 个端点` : '未声明，回落到内置端点表'
}

export function audioVideoSummary(config: TypeConfigRecord): string {
  const audio = AUDIO_ADAPTER_OPTIONS.find((item) => item.value === String(config?.audio?.adapter ?? 'openai'))?.label ?? '标准 OpenAI 音频端点'
  const video = VIDEO_ADAPTER_OPTIONS.find((item) => item.value === String(config?.video?.adapter ?? 'openai'))?.label ?? '标准 OpenAI 视频端点'
  return `${audio} · ${video}`
}

export function protocolSummary(config: TypeConfigRecord): string {
  return config?.protocol?.chatCompletionsOnly ? '仅 Chat Completions' : '按模型名自动选择端点'
}
