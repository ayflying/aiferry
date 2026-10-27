import type { ChannelType } from '../api/types'

const costAdapterLabels: Record<string, string> = {
  none: '不查询',
  openai_costs: 'OpenAI Costs',
  sub2api_usage: 'Sub2API 费用/余额',
  newapi_balance: 'NewAPI 余额',
  custom_json: '自定义 JSON',
  qiniu_costs: '七牛预估费用',
  siliconflow_balance: '硅基流动余额',
  openrouter_credits: 'OpenRouter 余额',
  qiniu_usage: '七牛用量',
  workbuddy_credits: '积分余额',
}

export function channelTypeCostLabel(channelType: Pick<ChannelType, 'config'>) {
  const adapter = channelType.config.costs.adapter
  return costAdapterLabels[adapter] || adapter
}

export function channelTypeQueryLabel(channelType: Pick<ChannelType, 'config'>) {
  const { adapter, valueType } = channelType.config.costs
  if (adapter === 'none') return '不查询'
  if (adapter === 'workbuddy_credits') return '积分余额查询'
  if (valueType === 'usage' || adapter === 'qiniu_usage') return 'Token/积分用量查询'
  return '费用/余额查询'
}

// 计数口径适配器：数值是额度/积分/Token，不是金额，不能套货币与汇率。
const usageAdapters = ['qiniu_usage', 'workbuddy_credits']

export function isUsageAdapter(channelType: Pick<ChannelType, 'config'> | undefined) {
  const costs = channelType?.config.costs
  return costs?.valueType === 'usage' || usageAdapters.includes(costs?.adapter || '')
}

export function isUsageMode(queryType?: string, queryMode?: string) {
  return queryType === 'usage' || usageAdapters.includes(queryMode || '')
}

// isCreditsQueryType 判断渠道查询的是「积分余额」（workbuddy_credits）而不是
// 货币余额。积分与网关按定价累计的钱是两种单位，展示时必须分开合并，否则会
// 出现「USD 积分余额 剩余 0.04 积分」这种自相矛盾的汇总。
export function isCreditsQueryType(queryType?: string) {
  return queryType === 'workbuddy_credits'
}

export function channelQueryValueLabel(queryType?: string, queryMode?: string) {
  if (queryMode === 'workbuddy_credits') return '积分余额'
  if (isUsageMode(queryType, queryMode)) return '用量'
  if (queryMode === 'qiniu_costs') return '预估费用'
  return queryMode === 'newapi_balance' || queryMode === 'siliconflow_balance' || queryMode === 'openrouter_credits' ? '余额' : '费用'
}
