import { ElMessageBox } from 'element-plus'
import 'element-plus/es/components/message-box/style/css'

const fallbackMessage = '操作失败，请稍后重试'

export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim()) return error.message
  if (typeof error === 'string' && error.trim()) return error
  return fallbackMessage
}

export function showError(error: unknown, title = '操作失败') {
  void ElMessageBox.alert(errorMessage(error), title, {
    type: 'error',
    confirmButtonText: '知道了',
    customClass: 'app-error-dialog',
    closeOnClickModal: false,
    closeOnPressEscape: false,
    showClose: false,
  }).catch(() => undefined)
}
