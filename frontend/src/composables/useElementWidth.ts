import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

/**
 * useElementWidth 跟踪元素内容盒的布局像素宽度。
 *
 * 表格列宽用的是 CSS 布局像素，因此这里必须读同一坐标系：
 * `getBoundingClientRect().width` 返回的是视觉宽度，祖先节点存在 transform: scale
 * 或 CSS zoom 时会被缩放，据此算列宽会与 ElTable 实际布局对不上（例如误加列后超框）。
 * ResizeObserver 的 contentRect / clientWidth 都是布局像素，优先使用。
 *
 * 不支持 ResizeObserver 的环境（如 jsdom 单测）退化为只在挂载时测一次。
 */
export function useElementWidth(target: Ref<HTMLElement | undefined>): Ref<number> {
  const width = ref(0)
  let observer: ResizeObserver | undefined

  function measure(entry?: ResizeObserverEntry) {
    const element = target.value
    if (!element) return
    const contentWidth = entry?.contentRect?.width ?? 0
    width.value = contentWidth || element.clientWidth || element.getBoundingClientRect().width
  }

  onMounted(() => {
    measure()
    if (typeof ResizeObserver === 'undefined' || !target.value) return
    observer = new ResizeObserver((entries) => measure(entries[0]))
    observer.observe(target.value)
  })
  onBeforeUnmount(() => observer?.disconnect())

  return width
}
