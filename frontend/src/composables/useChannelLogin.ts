import { onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { apiPost } from '../api/client'
import type { ChannelLoginInput, ChannelLoginResult, ChannelLoginSession } from '../api/types'
import { errorMessage, showError, showSuccess } from '../lib/error'

/** 一次只保留一个登录会话；等待授权时串行轮询，选择项目时等用户主动确认。 */
export function useChannelLogin(channelID: () => number | undefined, active: () => boolean, completed: () => Promise<void>) {
  const visible = ref(false)
  const starting = ref(false)
  const session = shallowRef<ChannelLoginSession | null>(null)
  const error = ref('')
  const elapsed = ref(0)
  const expiresIn = ref(300)
  const projectStep = shallowRef<Extract<ChannelLoginResult, { status: 'project_required' }> | null>(null)
  const confirmingProject = ref(false)
  const manualRetryAvailable = ref(false)
  let generation = 0
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
    confirmingProject.value = false
  }

  function resetSession() {
    stop()
    session.value = null
    projectStep.value = null
    manualRetryAvailable.value = false
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

  function pollLogin(id: number, state: string, projectId?: string) {
    return apiPost<ChannelLoginResult>(`/channels/${id}/credentials/login/poll`, projectId ? { state, projectId } : { state })
  }

  async function confirmProject(rawProjectId: string) {
    const id = channelID()
    const state = session.value?.state
    const projectId = rawProjectId.trim()
    if (!id || !state || !visible.value || !active() || !projectId || !projectStep.value || confirmingProject.value) return
    const token = generation
    confirmingProject.value = true
    error.value = ''
    try {
      const status = await pollLogin(id, state, projectId)
      if (token !== generation) return
      if (status.status === 'completed') await finish()
      else error.value = '项目尚未保存，请再次确认。'
    } catch (cause) {
      if (token === generation) error.value = errorMessage(cause)
    } finally {
      if (token === generation) confirmingProject.value = false
    }
  }

  async function start(input?: ChannelLoginInput, allowManualRetry = false) {
    const id = channelID()
    if (!id || !active() || starting.value || confirmingProject.value) return
    resetSession()
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
      const interval = positiveSeconds(result.intervalSeconds, result.userCode ? 5 : 2) * 1000
      const deadline = Date.now() + expiresIn.value * 1000
      visible.value = true
      expiryTimer = setTimeout(expire, expiresIn.value * 1000)
      elapsedTimer = setInterval(() => { elapsed.value = Math.floor((Date.now() - (deadline - expiresIn.value * 1000)) / 1000) }, 1000)
      const poll = async () => {
        if (token !== generation) return
        if (Date.now() >= deadline) { expire(); return }
        try {
          const status = await pollLogin(id, result.state)
          if (token !== generation) return
          if (Date.now() >= deadline) { expire(); return }
          if (status.status === 'completed') {
            await finish(input?.credentialId)
          } else if (status.status === 'project_required') {
            stopPolling()
            projectStep.value = status
          } else {
            pollTimer = setTimeout(() => { void poll() }, interval)
          }
        } catch (cause) {
          if (token !== generation) return
          stop()
          manualRetryAvailable.value = allowManualRetry
          if (allowManualRetry) error.value = errorMessage(cause)
          else {
            showError(cause, '登录轮询失败')
            error.value = '轮询失败，可点「重新发起登录」重试'
          }
        }
      }
      pollTimer = setTimeout(() => { void poll() }, interval)
      if (loginWindow && !loginWindow.closed) loginWindow.location.replace(result.authUrl)
    } catch (cause) {
      loginWindow?.close()
      if (token === generation) {
        stop()
        showError(cause, '发起登录失败')
        if (visible.value && (allowManualRetry || input?.projectId)) {
          manualRetryAvailable.value = true
          error.value = errorMessage(cause)
        }
      }
    } finally {
      if (token === generation) starting.value = false
    }
  }

  watch(visible, (open) => { if (!open) resetSession() }, { flush: 'sync' })
  watch([channelID, active], () => {
    if (visible.value) visible.value = false
    else resetSession()
  }, { flush: 'sync' })
  onBeforeUnmount(stop)
  return { visible, starting, session, error, elapsed, expiresIn, projectStep, confirmingProject, manualRetryAvailable, open, start, confirmProject }
}

function positiveSeconds(value: number | undefined, fallback: number) {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : fallback
}
