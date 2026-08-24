import type { ChannelType } from '../api/types'

const costAdapterLabels: Record<string, string> = {
  none: '不查询',
  openai_costs: 'OpenAI Costs',
  sub2api_usage: 'Sub2API 费用/余额',
  newapi_balance: 'NewAPI 余额',
  custom_json: '自定义 JSON',
  qiniu_usage: '七牛用量',
}

export function channelTypeCostLabel(channelType: Pick<ChannelType, 'config'>) {
  const adapter = channelType.config.costs.adapter
  return costAdapterLabels[adapter] || adapter
}

export function channelTypeQueryLabel(channelType: Pick<ChannelType, 'config'>) {
  const { adapter, valueType } = channelType.config.costs
  if (adapter === 'none') return '不查询'
  if (valueType === 'usage' || adapter === 'qiniu_usage') return 'Token/积分用量查询'
  return '费用/余额查询'
}

export function isUsageAdapter(channelType: Pick<ChannelType, 'config'> | undefined) {
  const costs = channelType?.config.costs
  return costs?.valueType === 'usage' || costs?.adapter === 'qiniu_usage'
}

export function isUsageMode(queryType?: string, queryMode?: string) {
  return queryType === 'usage' || queryMode === 'qiniu_usage'
}

export function channelQueryValueLabel(queryType?: string, queryMode?: string) {
  if (isUsageMode(queryType, queryMode)) return '用量'
  return queryMode === 'newapi_balance' ? '余额' : '费用'
}
