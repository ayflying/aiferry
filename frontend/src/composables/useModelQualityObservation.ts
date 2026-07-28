import { reactive, ref } from 'vue'
import { apiGet, apiPut } from '../api/client'
import type { ModelQualityEventPage, ModelQualitySettings } from '../api/types'
import { showError, showSuccess } from '../lib/error'

export function useModelQualityObservation(onEnabledChange: (enabled: boolean) => void) {
  const saving = ref(false)
  const eventsLoading = ref(false)
  const settings = reactive<ModelQualitySettings>({ enabled: false })
  const events = ref<ModelQualityEventPage>({ items: [], total: 0 })
  const filters = reactive({ page: 1, pageSize: 50 })

  async function load() {
    try {
      const [nextSettings, nextEvents] = await Promise.all([
        apiGet<ModelQualitySettings>('/system/model-quality'),
        apiGet<ModelQualityEventPage>('/system/model-quality/events', filters),
      ])
      Object.assign(settings, nextSettings)
      events.value = nextEvents
    } catch (error) { showError(error, '加载模型质量观测失败') }
  }

  async function refresh() {
    eventsLoading.value = true
    try {
      events.value = await apiGet<ModelQualityEventPage>('/system/model-quality/events', filters)
    } catch (error) { showError(error, '加载模型质量历史失败') } finally { eventsLoading.value = false }
  }

  async function updateEnabled(enabled: boolean) {
    const previous = settings.enabled
    settings.enabled = enabled
    saving.value = true
    try {
      Object.assign(settings, await apiPut<ModelQualitySettings>('/system/model-quality', { enabled }))
      onEnabledChange(settings.enabled)
      showSuccess(settings.enabled ? '模型质量检测已开启' : '模型质量检测已关闭', '保存成功')
    } catch (error) {
      settings.enabled = previous
      showError(error, '保存模型质量检测设置失败')
    } finally { saving.value = false }
  }

  return { eventsLoading, events, filters, load, refresh, saving, settings, updateEnabled }
}
