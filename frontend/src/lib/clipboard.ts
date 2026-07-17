export async function copyText(text: string): Promise<void> {
  if (!text) throw new Error('没有可复制的内容')

  const clipboard = typeof navigator === 'undefined' ? undefined : navigator.clipboard
  if (clipboard?.writeText) {
    try {
      await clipboard.writeText(text)
      return
    } catch {
      // HTTP 和嵌入式浏览器可能暴露 Clipboard API，但拒绝实际写入。
    }
  }

  if (copyWithLegacyCommand(text)) return
  throw new Error('当前浏览器不支持复制，请手动复制密钥')
}

function copyWithLegacyCommand(text: string): boolean {
  if (typeof document === 'undefined' || !document.body || typeof document.execCommand !== 'function') return false

  const input = document.createElement('textarea')
  input.value = text
  input.setAttribute('readonly', '')
  input.style.position = 'fixed'
  input.style.top = '0'
  input.style.left = '-9999px'
  document.body.appendChild(input)
  input.focus()
  input.select()
  input.setSelectionRange(0, input.value.length)

  try {
    return document.execCommand('copy')
  } finally {
    input.remove()
  }
}
