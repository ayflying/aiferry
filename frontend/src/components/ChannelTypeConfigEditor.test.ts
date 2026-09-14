import { mount, type VueWrapper } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { nextTick } from 'vue'

import ChannelTypeConfigEditor from './ChannelTypeConfigEditor.vue'

const BASE_CONFIG = {
  baseUrl: 'https://a.example.com/v1',
  models: { method: 'GET', path: '/models', listPath: 'data', idPath: 'id', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ' },
  costs: { adapter: 'none', valueType: 'cost', method: 'GET', path: '', authType: 'management_key', headerName: 'Authorization', headerPrefix: 'Bearer ' },
  pricing: { adapter: 'none' },
  endpoints: { chatCompletions: { method: 'POST', path: '/chat/completions', requestBody: 'json', supportsStream: true, authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ' } },
  // 表单没有渲染这个键，提交时必须原样保留，否则后端 DisallowUnknownFields 之外的新字段会被抹掉
  futureKey: { keep: true },
}

const BASE_TEXT = JSON.stringify(BASE_CONFIG, null, 2)

/** 模拟父级的 v-model 双向绑定：组件写回文本会立刻回灌成 props。 */
function mountWithModel(initial: string, readonly = false): VueWrapper {
  let wrapper: VueWrapper
  wrapper = mount(ChannelTypeConfigEditor, {
    props: {
      configText: initial,
      readonly,
      'onUpdate:configText': (value: string) => { void wrapper.setProps({ configText: value }) },
    },
  })
  return wrapper
}

function baseUrlInput(wrapper: VueWrapper) {
  return wrapper.get('input[placeholder="https://api.example.com/v1"]')
}

function lastEmittedConfig(wrapper: VueWrapper): Record<string, any> {
  const events = wrapper.emitted('update:configText') as string[][] | undefined
  expect(events?.length).toBeTruthy()
  return JSON.parse(events![events!.length - 1][0])
}

describe('channel type config editor', () => {
  it('第一次渲染不写回文本，避免打开抽屉就改动用户的配置', () => {
    const wrapper = mountWithModel(BASE_TEXT)
    expect(wrapper.emitted('update:configText')).toBeUndefined()
  })

  it('表单改动写回 JSON，并保留未渲染的键与端点', async () => {
    const wrapper = mountWithModel(BASE_TEXT)
    await baseUrlInput(wrapper).setValue('https://b.example.com/v1')
    await nextTick()
    const config = lastEmittedConfig(wrapper)
    expect(config.baseUrl).toBe('https://b.example.com/v1')
    expect(config.futureKey).toEqual({ keep: true })
    expect(config.endpoints.chatCompletions.path).toBe('/chat/completions')
  })

  it('父级「先清空再回填」时回填必须重建表单，回声只吞一次', async () => {
    const wrapper = mountWithModel(BASE_TEXT)
    await baseUrlInput(wrapper).setValue('https://b.example.com/v1')
    await nextTick()
    const writtenBack = lastEmittedConfig(wrapper)
    expect(wrapper.get('input[placeholder="https://api.example.com/v1"]').element).toHaveProperty('value', 'https://b.example.com/v1')

    // 打开新增类型时父级会把文本清空，再把默认配置异步写进来
    await wrapper.setProps({ configText: '' })
    await nextTick()
    expect(wrapper.get('input[placeholder="https://api.example.com/v1"]').element).toHaveProperty('value', '')

    // 回填内容恰好等于上一次写回的文本：仍然必须重建，否则表单会停在空骨架
    await wrapper.setProps({ configText: JSON.stringify(writtenBack, null, 2) })
    await nextTick()
    expect(wrapper.get('input[placeholder="https://api.example.com/v1"]').element).toHaveProperty('value', 'https://b.example.com/v1')
  })

  it('切到 JSON 视图显示的是表单最新内容，切回表单也能读回 JSON 的改动', async () => {
    const wrapper = mountWithModel(BASE_TEXT)
    await baseUrlInput(wrapper).setValue('https://c.example.com/v1')
    await nextTick()

    const radios = wrapper.findAll('input[type="radio"]')
    await radios[1].setValue()
    await nextTick()
    const textarea = wrapper.get('textarea')
    expect((textarea.element as HTMLTextAreaElement).value).toContain('https://c.example.com/v1')

    await textarea.setValue(JSON.stringify({ ...BASE_CONFIG, baseUrl: 'https://d.example.com/v1' }, null, 2))
    await nextTick()
    await radios[0].setValue()
    await nextTick()
    expect(wrapper.get('input[placeholder="https://api.example.com/v1"]').element).toHaveProperty('value', 'https://d.example.com/v1')
  })

  it('只读（查看内置类型）时表单与 JSON 都不可编辑', async () => {
    const wrapper = mountWithModel(BASE_TEXT, true)
    expect(baseUrlInput(wrapper).attributes('disabled')).toBeDefined()
    await wrapper.findAll('input[type="radio"]')[1].setValue()
    await nextTick()
    expect(wrapper.get('textarea').attributes('readonly')).toBeDefined()
  })

  it('未声明端点时给出说明并可添加端点', async () => {
    const wrapper = mountWithModel(JSON.stringify({ baseUrl: '', models: { method: 'GET', path: '/models', listPath: 'data', idPath: 'id', authType: 'channel_key', headerName: 'Authorization', headerPrefix: 'Bearer ' } }))
    expect(wrapper.text()).toContain('当前未自定义端点')
    const addButton = wrapper.findAll('button').find((button) => button.text().includes('添加端点'))
    await addButton!.trigger('click')
    await nextTick()
    const config = lastEmittedConfig(wrapper)
    expect(Object.keys(config.endpoints)).toEqual(['customEndpoint1'])
  })
})
