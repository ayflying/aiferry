import { describe, expect, it } from 'vitest'
import { isHTTPURL, renderSiteContent } from './site-content'

describe('site content', () => {
  it('renders Markdown while removing executable markup', () => {
    const html = renderSiteContent('# Welcome\n\n<img src="x" onerror="alert(1)"><script>alert(1)</script>[bad](javascript:alert(1))')

    expect(html).toContain('<h1>Welcome</h1>')
    expect(html).not.toContain('onerror')
    expect(html).not.toContain('<script')
    expect(html).not.toContain('javascript:')
  })

  it('accepts only clean absolute HTTP(S) URLs', () => {
    expect(isHTTPURL('https://example.com/page')).toBe(true)
    expect(isHTTPURL('http://example.com')).toBe(true)
    expect(isHTTPURL('https://user@example.com')).toBe(false)
    expect(isHTTPURL('javascript:alert(1)')).toBe(false)
  })
})
