import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ElMessageBox } from 'element-plus'
import { errorMessage, showError, showInfo, showSuccess, showWarning } from './error'

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

  it('opens the shared success dialog with consistent options', () => {
    showSuccess('系统设置已保存', '保存成功')

    expect(ElMessageBox.alert).toHaveBeenCalledWith(
      '系统设置已保存',
      '保存成功',
      expect.objectContaining({
        type: 'success',
        confirmButtonText: '知道了',
        closeOnClickModal: false,
        closeOnPressEscape: false,
        showClose: false,
      }),
    )
  })

  it('opens the shared warning dialog with consistent options', () => {
    showWarning('端点名不可用', '注意')

    expect(ElMessageBox.alert).toHaveBeenCalledWith(
      '端点名不可用',
      '注意',
      expect.objectContaining({
        type: 'warning',
        confirmButtonText: '知道了',
        closeOnClickModal: false,
        closeOnPressEscape: false,
        showClose: false,
      }),
    )
  })

  it('opens the shared info dialog with consistent options', () => {
    showInfo('没有可删除的无效兑换码', '提示')

    expect(ElMessageBox.alert).toHaveBeenCalledWith(
      '没有可删除的无效兑换码',
      '提示',
      expect.objectContaining({
        type: 'info',
        confirmButtonText: '知道了',
        closeOnClickModal: false,
        closeOnPressEscape: false,
        showClose: false,
      }),
    )
  })
})
