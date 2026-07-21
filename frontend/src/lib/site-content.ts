import DOMPurify from 'dompurify'
import { marked } from 'marked'

const httpURL = /^https?:\/\//i

export function isHTTPURL(value: string) {
  try {
    const url = new URL(value.trim())
    return (url.protocol === 'http:' || url.protocol === 'https:') && !url.username && !url.password
  } catch {
    return false
  }
}

export function renderSiteContent(value: string) {
  const source = value.trim()
  if (!source) return ''

  const restrictEmbeddedContent = (node: Node) => {
    const element = node as Element
    if (!element.tagName) return
    const tagName = element.tagName.toLowerCase()
    if (tagName === 'iframe') {
      const src = element.getAttribute('src') || ''
      if (!isHTTPURL(src)) {
        element.remove()
        return
      }
      element.setAttribute('sandbox', '')
      element.setAttribute('referrerpolicy', 'no-referrer')
      element.setAttribute('loading', 'lazy')
      return
    }
    if (tagName === 'a') {
      element.setAttribute('target', '_blank')
      element.setAttribute('rel', 'noopener noreferrer')
    }
  }

  DOMPurify.addHook('afterSanitizeAttributes', restrictEmbeddedContent)
  try {
    return DOMPurify.sanitize(marked.parse(source, { async: false, breaks: true, gfm: true }) as string, {
      ADD_TAGS: ['iframe'],
      ADD_ATTR: ['allow', 'allowfullscreen', 'loading', 'referrerpolicy', 'sandbox'],
      FORBID_ATTR: ['srcdoc', 'style'],
      FORBID_TAGS: ['base', 'embed', 'form', 'object', 'script', 'style'],
    })
  } finally {
    DOMPurify.removeHook('afterSanitizeAttributes')
  }
}
