const authErrors: Record<string, string> = {
  access_denied: '当前账号不具备系统访问权限',
  invalid_state: '登录请求已过期，请重新发起登录',
  auth_failed: 'Casdoor 认证失败，请稍后重试',
  auth_unavailable: '认证服务暂时不可用，请稍后重试',
}

export function localReturnTo(value: unknown): string {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || /[\r\n]/.test(value)) return '/'
  return value
}

export function authErrorMessage(value: unknown): string {
  if (typeof value !== 'string') return ''
  return authErrors[value] || '登录未完成，请重新尝试'
}
