import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { apiPost } from '../api/client'
import type { ChannelLoginInput, ChannelLoginResult, ChannelLoginSession } from '../api/types'
import { errorMessage, showError, showSuccess } from '../lib/error'

/** 一次只保留一个登录会话；等待授权时串行轮询。 */
export function useChannelLogin(
  channelID: () => number | undefined,
  active: () => boolean,
  completed: () => Promise<void>,
  // 回调落在管理员本机、服务端收不到时（Antigravity 桥接），改用「提交回调地址」而不是轮询。
  callbackRequired: () => boolean = () => false,
) {
  const visible = ref(false)
  const starting = ref(false)
  const completing = ref(false)
  const session = shallowRef<ChannelLoginSession | null>(null)
  const error = ref('')
  const elapsed = ref(0)
  const expiresIn = ref(300)
  let generation = 0
  // 发起这次登录的凭据行；提交回调地址时必须用同一行，否则会落到别的账号上。
  let activeCredentialID: number | undefined
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  let elapsedTimer: ReturnType<typeof setInterval> | undefined

  function stopPolling() {
    clearTimeout(pollTimer)
    clearTimeout(expiryTimer)
    clearInterval(elapsedTimer)
  }

  function stop() {
    generation += 1
    stopPolling()
    starting.value = false
  }

  function resetSession() {
    stop()
    session.value = null
    error.value = ''
    elapsed.value = 0
  }

  function expire() {
    stop()
    error.value = '登录地址已超时，请点「重新发起登录」重新获取地址'
  }

  function open() {
    resetSession()
    visible.value = true
  }

  async function finish(credentialId?: number) {
    visible.value = false
    showSuccess(credentialId ? '重新登录成功，账号凭据已更新' : '登录成功，凭据已写入该渠道')
    await completed()
  }

  async function start(input?: ChannelLoginInput) {
    const id = channelID()
    if (!id || !active() || starting.value) return
    resetSession()
    activeCredentialID = input?.credentialId
    const token = generation
    const loginWindow = window.open('about:blank', '_blank')
    if (loginWindow) loginWindow.opener = null
    starting.value = true
    try {
      const path = `/channels/${id}/credentials/login`
      const result = input ? await apiPost<ChannelLoginSession>(path, input) : await apiPost<ChannelLoginSession>(path)
      if (token !== generation || id !== channelID() || !active()) {
        loginWindow?.close()
        return
      }
      session.value = result
      expiresIn.value = positiveSeconds(result.expiresInSeconds, 300)
      const deadline = Date.now() + expiresIn.value * 1000
      visible.value = true
      expiryTimer = setTimeout(expire, expiresIn.value * 1000)
      elapsedTimer = setInterval(() => { elapsed.value = Math.floor((Date.now() - (deadline - expiresIn.value * 1000)) / 1000) }, 1000)
      if (loginWindow && !loginWindow.closed) loginWindow.location.replace(result.authUrl)
      // 回调打在管理员本机，服务端等不到「自动完成」，这类渠道改为等待提交回调地址。
      if (callbackRequired()) return
      const interval = positiveSeconds(result.intervalSeconds, result.userCode ? 5 : 2) * 1000
      const poll = async () => {
        if (token !== generation) return
        if (Date.now() >= deadline) { expire(); return }
        try {
          const status = await apiPost<ChannelLoginResult>(`/channels/${id}/credentials/login/poll`, { state: result.state })
          if (token !== generation) return
          if (Date.now() >= deadline) { expire(); return }
          if (status.status === 'completed') {
            await finish(input?.credentialId)
          } else {
            pollTimer = setTimeout(() => { void poll() }, interval)
          }
        } catch (cause) {
          if (token !== generation) return
          stop()
          showError(cause, '登录轮询失败')
          error.value = '轮询失败，可点「重新发起登录」重试'
        }
      }
      pollTimer = setTimeout(() => { void poll() }, interval)
    } catch (cause) {
      loginWindow?.close()
      if (token === generation) {
        stop()
        if (visible.value) error.value = errorMessage(cause)
        else showError(cause, '发起登录失败')
      }
    } finally {
      if (token === generation) starting.value = false
    }
  }

  /** 提交浏览器授权后的回调地址，完成桥接自管的登录（回调不经过服务端）。 */
  async function complete(callbackUrl: string) {
    const id = channelID()
    const current = session.value
    if (!id || !current || !active() || completing.value) return
    completing.value = true
    error.value = ''
    try {
      await apiPost<ChannelLoginResult>(`/channels/${id}/credentials/login/complete`, { state: current.state, callbackUrl, credentialId: activeCredentialID })
      await finish()
    } catch (cause) {
      error.value = errorMessage(cause)
      showError(cause, '提交授权回调地址失败')
    } finally {
      completing.value = false
    }
  }

  watch(visible, (open) => { if (!open) resetSession() }, { flush: 'sync' })
  watch([channelID, active], () => {
    if (visible.value) visible.value = false
    else resetSession()
  }, { flush: 'sync' })
  onBeforeUnmount(stop)
  return { visible, starting, completing, session, error, elapsed, expiresIn, open, start, complete }
}

function positiveSeconds(value: number | undefined, fallback: number) {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : fallback
}
