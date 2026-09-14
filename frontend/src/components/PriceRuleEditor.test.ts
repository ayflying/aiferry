import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'

import type { PriceRuleDraft } from '../lib/model-pricing'
import PriceRuleEditor from './PriceRuleEditor.vue'

function draft(overrides: Partial<PriceRuleDraft> = {}): PriceRuleDraft {
  return { name: '', priority: 100, currency: 'USD', conditions: {}, rates: {}, ...overrides }
}

function mountEditor(showActions?: boolean, modelValue: PriceRuleDraft = draft()) {
  return mount(PriceRuleEditor, { props: { modelValue, showActions } })
}

describe('PriceRuleEditor 操作区', () => {
  it('默认自带操作行，便于在没有弹框的容器里内联使用', () => {
    expect(mountEditor().find('.editor-actions').exists()).toBe(true)
  })

  // 新增/编辑都改到弹框里之后，提交与取消由弹框底部承载；此时编辑器自带的操作行必须隐藏，
  // 否则同一个弹框里会出现两组「保存/取消」，点错哪一组都不明显。
  it('showActions=false 时隐藏自带操作行', () => {
    expect(mountEditor(false).find('.editor-actions').exists()).toBe(false)
  })

  it('隐藏操作行不影响 modelValue 回填', async () => {
    const wrapper = mountEditor(false, draft({ name: '高峰时段', priority: 88, rates: { inputPerMillion: 0.3 } }))
    await nextTick()
    const fields = wrapper.findAll('input')
    expect(fields[0].element.value).toBe('高峰时段')
    expect(fields[1].element.value).toBe('88')
  })
})
