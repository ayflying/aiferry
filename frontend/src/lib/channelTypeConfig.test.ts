import { describe, expect, it } from 'vitest'

import {
  configForForm, costsShowsBalanceFields, costsShowsUsageFields, createEndpointConfig,
  endpointNames, endpointSummary, parseTypeConfigText, pricingDetailVisible,
  quotaDetailLocked, quotaDetailVisible, renameEndpoint,
} from './channelTypeConfig'

describe('channel type config helpers', () => {
  it('空文本表示沿用后端默认配置，而不是报格式错误', () => {
    expect(parseTypeConfigText('   ')).toEqual({ config: null, error: '' })
  })

  it('非对象与非法 JSON 都给出错误文案', () => {
    expect(parseTypeConfigText('[1,2]').error).toContain('JSON 对象')
    expect(parseTypeConfigText('{"a":1').error).not.toBe('')
  })

  it('解析结果原样返回，不做字段过滤', () => {
    const { config, error } = parseTypeConfigText('{"baseUrl":"https://a/v1","unknownKey":1}')
    expect(error).toBe('')
    expect(config).toEqual({ baseUrl: 'https://a/v1', unknownKey: 1 })
  })

  it('补齐表单需要的最小骨架，同时保留未知键', () => {
    const form = configForForm({ baseUrl: 'https://a/v1', futureField: { x: 1 } })
    expect(form.models).toMatchObject({ method: 'GET', authType: 'channel_key' })
    expect(form.costs).toMatchObject({ adapter: 'none' })
    expect(form.protocol).toMatchObject({ chatCompletionsOnly: false })
    expect(form.futureField).toEqual({ x: 1 })
  })

  it('不补 endpoints：缺失时后端会回落到内置端点表，补成空对象反而会校验失败', () => {
    const form = configForForm({ baseUrl: '' })
    expect('endpoints' in form).toBe(false)
    const withEndpoints = configForForm({ endpoints: { chatCompletions: { path: '/chat/completions' } } })
    expect(Object.keys(withEndpoints.endpoints)).toEqual(['chatCompletions'])
  })

  it('草稿与传入的配置解耦，改草稿不会污染原对象', () => {
    const source = { baseUrl: 'https://a/v1', models: { path: '/models' } }
    const form = configForForm(source)
    form.models.path = '/v2/models'
    form.baseUrl = 'https://b/v1'
    expect(source.models.path).toBe('/models')
    expect(source.baseUrl).toBe('https://a/v1')
  })

  it('费用字段按适配器与查询语义切换：用量、余额、未启用三态互斥', () => {
    expect(costsShowsBalanceFields({ costs: { adapter: 'openai_costs', valueType: 'cost' } })).toBe(true)
    expect(costsShowsUsageFields({ costs: { adapter: 'openai_costs', valueType: 'cost' } })).toBe(false)

    expect(costsShowsUsageFields({ costs: { adapter: 'custom_json', valueType: 'usage' } })).toBe(true)
    expect(costsShowsBalanceFields({ costs: { adapter: 'custom_json', valueType: 'usage' } })).toBe(false)

    // 旧版七牛用量适配器即使没写 valueType 也按用量处理，与后端 IsUsageCost 对齐。
    expect(costsShowsUsageFields({ costs: { adapter: 'qiniu_usage' } })).toBe(true)

    expect(costsShowsBalanceFields({ costs: { adapter: 'none' } })).toBe(false)
    expect(costsShowsUsageFields({ costs: { adapter: 'none' } })).toBe(false)
    expect(costsShowsBalanceFields({})).toBe(false)
  })

  it('价格同步明细与套餐额度明细只在启用时展示', () => {
    expect(pricingDetailVisible({ pricing: { adapter: 'json' } })).toBe(true)
    expect(pricingDetailVisible({ pricing: { adapter: 'none' } })).toBe(false)
    expect(pricingDetailVisible({})).toBe(false)

    expect(quotaDetailVisible({ quota: { adapter: 'zhipu_coding_plan' } })).toBe(true)
    expect(quotaDetailVisible({ quota: { adapter: 'none' } })).toBe(false)
    // OpenCode Go 与火山 AFP 的请求方式、鉴权由后端固定覆盖，字段只读。
    expect(quotaDetailLocked({ quota: { adapter: 'opencode_go_usage' } })).toBe(true)
    expect(quotaDetailLocked({ quota: { adapter: 'volcengine_afp' } })).toBe(true)
    expect(quotaDetailLocked({ quota: { adapter: 'zhipu_coding_plan' } })).toBe(false)
  })

  it('端点重命名保持顺序、拒绝重名与空名', () => {
    const config = { endpoints: { chatCompletions: { path: '/a' }, responses: { path: '/b' } } }
    expect(renameEndpoint(config, 'chatCompletions', 'chat')).toBe(true)
    expect(Object.keys(config.endpoints)).toEqual(['chat', 'responses'])
    expect(renameEndpoint(config, 'chat', 'responses')).toBe(false)
    expect(renameEndpoint(config, 'chat', '   ')).toBe(false)
    expect(renameEndpoint(config, 'chat', 'chat')).toBe(false)
    expect(Object.keys(config.endpoints)).toEqual(['chat', 'responses'])
  })

  it('新增端点带一份可直接使用的默认值', () => {
    expect(createEndpointConfig()).toMatchObject({ method: 'POST', requestBody: 'json', authType: 'channel_key' })
    expect(createEndpointConfig()).toMatchObject({ headerName: 'Authorization', headerPrefix: 'Bearer ' })
  })

  it('端点摘要区分「未声明」与具体数量', () => {
    expect(endpointNames({})).toEqual([])
    expect(endpointSummary({})).toBe('未声明，回落到内置端点表')
    expect(endpointSummary({ endpoints: { a: {}, b: {} } })).toBe('2 个端点')
  })
})
