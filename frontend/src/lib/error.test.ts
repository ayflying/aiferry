import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ElMessageBox } from 'element-plus'
import { errorMessage, showError } from './error'

vi.mock('element-plus', () => ({
  ElMessageBox: { alert: vi.fn(() => Promise.resolve()) },
}))

describe('errorMessage', () => {
  beforeEach(() => vi.mocked(ElMessageBox.alert).mockClear())

  it('returns the server or application error message', () => {
    expect(errorMessage(new Error('渠道类型没有配置价格同步接口'))).toBe('渠道类型没有配置价格同步接口')
    expect(errorMessage('网络请求失败')).toBe('网络请求失败')
  })

  it('uses a readable fallback for unknown errors', () => {
    expect(errorMessage(undefined)).toBe('操作失败，请稍后重试')
  })

  it('opens the shared error dialog with consistent options', () => {
    showError(new Error('渠道类型没有配置价格同步接口'), '价格同步失败')

    expect(ElMessageBox.alert).toHaveBeenCalledWith(
      '渠道类型没有配置价格同步接口',
      '价格同步失败',
      expect.objectContaining({
        type: 'error',
        confirmButtonText: '知道了',
        closeOnClickModal: false,
        closeOnPressEscape: false,
        showClose: false,
      }),
    )
  })
})
