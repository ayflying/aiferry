import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import SiteFooter from './SiteFooter.vue'
import { useSystemStore } from '../stores/system'

function footer() {
  return mount(SiteFooter)
}

describe('站点页脚版本号', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  // 页脚版本号是判断「线上镜像有没有真的换掉」的唯一直观入口，必须展示。
  it('服务端下发版本时展示 v 前缀的版本号', () => {
    const site = useSystemStore()
    site.apply({ systemName: 'AiFerry', version: '0.7.21', revision: 'abc1234' })
    const wrapper = footer()
    expect(wrapper.find('.footer-version').text()).toBe('v0.7.21')
  })

  // 悬停给出提交号，方便核对是否与本次提交一致。
  it('提交号通过 title 暴露，不占正文', () => {
    const site = useSystemStore()
    site.apply({ systemName: 'AiFerry', version: '0.7.21', revision: 'abc1234' })
    const wrapper = footer()
    expect(wrapper.find('.footer-version').attributes('title')).toBe('revision abc1234')
  })

  // 版本未知时不要渲染空的 v 占位，避免页脚出现孤立字母。
  it('没有版本信息时不渲染版本元素', () => {
    const site = useSystemStore()
    site.apply({ systemName: 'AiFerry' })
    const wrapper = footer()
    expect(wrapper.find('.footer-version').exists()).toBe(false)
  })

  // 自定义页脚文案不应影响版本展示。
  it('自定义页脚文案与版本号可同时存在', () => {
    const site = useSystemStore()
    site.apply({ systemName: 'AiFerry', footer: '示例页脚', version: '0.7.21' })
    const wrapper = footer()
    expect(wrapper.text()).toContain('示例页脚')
    expect(wrapper.find('.footer-version').text()).toBe('v0.7.21')
  })
})
