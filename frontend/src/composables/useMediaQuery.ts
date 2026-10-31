import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

/**
 * useMediaQuery 订阅一条媒体查询并在视口变化时更新布尔值。
 *
 * 统一封装 matchMedia 的挂载/卸载逻辑，避免每个页面各自维护监听器导致泄漏；
 * ResponsiveList 等组件与本函数共用同一套实现。
 *
 * @param source 媒体查询字符串，或返回该字符串的函数（传函数时仅在挂载时求值一次）。
 * @param fallback 客户端尚未挂载（如 SSR/单测首帧）时的兜底值。
 */
export function useMediaQuery(source: string | (() => string), fallback = false): Ref<boolean> {
  const matches = ref(fallback)
  const resolveQuery = typeof source === 'function' ? source : () => source
  let query: MediaQueryList | undefined

  function update() {
    matches.value = query?.matches ?? fallback
  }

  onMounted(() => {
    query = window.matchMedia(resolveQuery())
    update()
    query.addEventListener('change', update)
  })
  onBeforeUnmount(() => query?.removeEventListener('change', update))

  return matches
}
