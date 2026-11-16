import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ChannelCredentialDisplay from './ChannelCredentialDisplay.vue'

function display(props: Record<string, unknown>) {
  return mount(ChannelCredentialDisplay, { props })
}

describe('渠道密钥单元格', () => {
  it('没有备注时只展示渠道序号与访问密钥', () => {
    const wrapper = display({ channelName: 'adesk', credentialIndex: 1, apiKeyName: 'dsh' })
    expect(wrapper.text()).toContain('adesk #1')
    expect(wrapper.text()).toContain('dsh')
    expect(wrapper.find('.credential-remark').exists()).toBe(false)
  })

  it('有备注时把备注展示在密钥这一格', () => {
    const wrapper = display({ channelName: 'adesk', credentialIndex: 2, credentialRemark: 'hindsight 生产', apiKeyName: 'hindsight' })
    expect(wrapper.find('.credential-remark').text()).toBe('hindsight 生产')
    expect(wrapper.text()).toContain('adesk #2')
  })

  it('备注为空白时按没有备注处理，不留下空行', () => {
    const wrapper = display({ channelName: 'adesk', credentialIndex: 1, credentialRemark: '   ', apiKeyName: 'dsh' })
    expect(wrapper.find('.credential-remark').exists()).toBe(false)
  })
})
