import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

/**
 * useElementWidth 跟踪元素内容盒的实际像素宽度。
 *
 * 表格列是否放得下取决于容器真实宽度（侧栏折叠、窗口缩放都会变化），
 * 用视口宽度估算容易误判，因此统一用 ResizeObserver 测量。
 * 不支持 ResizeObserver 的环境（如 jsdom 单测）退化为只在挂载时测一次。
 */
export function useElementWidth(target: Ref<HTMLElement | undefined>): Ref<number> {
  const width = ref(0)
  let observer: ResizeObserver | undefined

  function measure() {
    const element = target.value
    if (!element) return
    width.value = element.getBoundingClientRect().width || element.clientWidth
  }

  onMounted(() => {
    measure()
    if (typeof ResizeObserver === 'undefined' || !target.value) return
    observer = new ResizeObserver(measure)
    observer.observe(target.value)
  })
  onBeforeUnmount(() => observer?.disconnect())

  return width
}
