import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { apiPost } from '../api/client'
import type { ChannelLoginResult, ChannelLoginSession } from '../api/types'
import { showError, showSuccess } from '../lib/error'

/** 一次只保留一个登录会话；轮询串行执行，并按服务端给定的间隔和有效期结束。 */
export function useChannelLogin(channelID: () => number | undefined, active: () => boolean, completed: () => Promise<void>) {
  const visible = ref(false)
  const starting = ref(false)
  const session = shallowRef<ChannelLoginSession | null>(null)
  const error = ref('')
  const elapsed = ref(0)
  const expiresIn = ref(300)
  let generation = 0
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  let elapsedTimer: ReturnType<typeof setInterval> | undefined

  function stop() {
    generation += 1
    clearTimeout(pollTimer)
    clearTimeout(expiryTimer)
    clearInterval(elapsedTimer)
    starting.value = false
  }

  function expire() {
    stop()
    error.value = '登录地址已超时，请点「重新发起登录」重新获取地址'
  }

  async function start() {
    const id = channelID()
    if (!id || !active() || starting.value) return
    stop()
    const token = generation
    const loginWindow = window.open('about:blank', '_blank')
    if (loginWindow) loginWindow.opener = null
    starting.value = true
    error.value = ''
    session.value = null
    try {
      const result = await apiPost<ChannelLoginSession>(`/channels/${id}/credentials/login`)
      if (token !== generation || id !== channelID() || !active()) {
        loginWindow?.close()
        return
      }
      session.value = result
      elapsed.value = 0
      expiresIn.value = positiveSeconds(result.expiresInSeconds, 300)
      const interval = positiveSeconds(result.intervalSeconds, result.userCode ? 5 : 2) * 1000
      const deadline = Date.now() + expiresIn.value * 1000
      visible.value = true
      expiryTimer = setTimeout(expire, expiresIn.value * 1000)
      elapsedTimer = setInterval(() => { elapsed.value = Math.floor((Date.now() - (deadline - expiresIn.value * 1000)) / 1000) }, 1000)
      const poll = async () => {
        if (token !== generation) return
        if (Date.now() >= deadline) { expire(); return }
        try {
          const status = await apiPost<ChannelLoginResult>(`/channels/${id}/credentials/login/poll`, { state: result.state })
          if (token !== generation) return
          if (Date.now() >= deadline) { expire(); return }
          if (status.status === 'completed') {
            stop()
            visible.value = false
            showSuccess('登录成功，凭据已写入该渠道')
            await completed()
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
      if (loginWindow && !loginWindow.closed) loginWindow.location.replace(result.authUrl)
    } catch (cause) {
      loginWindow?.close()
      if (token === generation) { stop(); showError(cause, '发起登录失败') }
    } finally {
      if (token === generation) starting.value = false
    }
  }

  watch(visible, (open) => { if (!open) { stop(); session.value = null } })
  watch([channelID, active], () => { stop(); visible.value = false; session.value = null }, { flush: 'sync' })
  onBeforeUnmount(stop)
  return { visible, starting, session, error, elapsed, expiresIn, start }
}

function positiveSeconds(value: number | undefined, fallback: number) {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : fallback
}
