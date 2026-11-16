import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ChannelCredentialDisplay from './ChannelCredentialDisplay.vue'

function display(props: Record<string, unknown>) {
  return mount(ChannelCredentialDisplay, { props })
}

describe('渠道密钥单元格', () => {
  it('没有备注时展示渠道序号与访问密钥', () => {
    const wrapper = display({ channelName: 'adesk', credentialIndex: 1, apiKeyName: 'dsh' })
    expect(wrapper.text()).toContain('adesk #1')
    expect(wrapper.text()).toContain('dsh')
  })

  it('有备注时用备注顶替序号', () => {
    const wrapper = display({ channelName: 'adesk', credentialIndex: 2, credentialRemark: 'hindsight 生产', apiKeyName: 'hindsight' })
    expect(wrapper.text()).toContain('adesk hindsight 生产')
    expect(wrapper.text()).not.toContain('#2')
  })

  it('备注首尾空白会被去掉', () => {
    const wrapper = display({ channelName: 'adesk', credentialIndex: 1, credentialRemark: '  dsh 主密钥  ' })
    expect(wrapper.text()).toContain('adesk dsh 主密钥')
  })

  it('备注为空白时按没有备注处理，仍显示序号', () => {
    const wrapper = display({ channelName: 'adesk', credentialIndex: 1, credentialRemark: '   ', apiKeyName: 'dsh' })
    expect(wrapper.text()).toContain('adesk #1')
  })

  it('已删除渠道有备注时同样用备注顶替', () => {
    const wrapper = display({ credentialIndex: 0, credentialRemark: '老密钥' })
    expect(wrapper.text()).toContain('已删除渠道 老密钥')
    expect(wrapper.text()).not.toContain('#未记录')
  })
})
