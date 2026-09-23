import { ElMessageBox } from 'element-plus'

const fallbackMessage = '操作失败，请稍后重试'

export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim()) return error.message
  if (typeof error === 'string' && error.trim()) return error
  return fallbackMessage
}

// 全站反馈弹框唯一入口：居中阻塞式（必须点「知道了」才关闭），不使用会掉到
// 页面底部的 ElMessage toast。四类语义共用一份 options，改样式只改这里。
type DialogKind = 'success' | 'warning' | 'info' | 'error'

function openDialog(message: string, title: string, kind: DialogKind, customClass: string) {
  void ElMessageBox.alert(message, title, {
    type: kind,
    confirmButtonText: '知道了',
    customClass,
    closeOnClickModal: false,
    closeOnPressEscape: false,
    showClose: false,
  }).catch(() => undefined)
}

export function showError(error: unknown, title = '操作失败') {
  openDialog(errorMessage(error), title, 'error', 'app-error-dialog')
}

export function showSuccess(message: string, title = '操作成功') {
  openDialog(message, title, 'success', 'app-success-dialog')
}

export function showWarning(message: string, title = '注意') {
  openDialog(message, title, 'warning', 'app-warning-dialog')
}

export function showInfo(message: string, title = '提示') {
  openDialog(message, title, 'info', 'app-info-dialog')
}
