import { describe, expect, it } from 'vitest'

import { supportsOrganizationIdentity } from './channelForm'

describe('channel form helpers', () => {
  it('只在 OpenAI 官方渠道展示组织 / 项目标识', () => {
    expect(supportsOrganizationIdentity({ config: { costs: { adapter: 'openai_costs' } } } as never)).toBe(true)
  })

  it('其它成本适配器一律不展示，避免填了上游也不认', () => {
    const adapters = [
      'none',
      '',
      'sub2api_usage',
      'qiniu_costs',
      'qiniu_usage',
      'siliconflow_balance',
      'openrouter_credits',
      'newapi_balance',
      'custom_adapter',
    ]
    for (const adapter of adapters) {
      expect(supportsOrganizationIdentity({ config: { costs: { adapter } } } as never)).toBe(false)
    }
  })

  it('渠道类型缺失或配置不全时安全返回 false', () => {
    expect(supportsOrganizationIdentity(undefined)).toBe(false)
    expect(supportsOrganizationIdentity({} as never)).toBe(false)
    expect(supportsOrganizationIdentity({ config: {} } as never)).toBe(false)
  })
})
