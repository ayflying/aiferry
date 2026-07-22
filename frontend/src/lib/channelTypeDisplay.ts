import type { ChannelType } from '../api/types'

const costAdapterLabels: Record<string, string> = {
  none: '不查询',
  openai_costs: 'OpenAI Costs',
  sub2api_usage: 'Sub2API Usage',
  newapi_balance: 'NewAPI 余额',
  custom_json: '自定义 JSON',
}

export function channelTypeCostLabel(channelType: Pick<ChannelType, 'config'>) {
  const adapter = channelType.config.costs.adapter
  return costAdapterLabels[adapter] || adapter
}
