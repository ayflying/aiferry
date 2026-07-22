import { describe, expect, it } from 'vitest'
import { authErrorMessage, localReturnTo } from './auth'

describe('localReturnTo', () => {
  it('keeps local application paths', () => {
    expect(localReturnTo('/channels?tab=models')).toBe('/channels?tab=models')
  })

  it('rejects external and malformed redirects', () => {
    expect(localReturnTo('https://example.com')).toBe('/dashboard')
    expect(localReturnTo('//example.com')).toBe('/dashboard')
    expect(localReturnTo('/ok\r\nLocation: https://example.com')).toBe('/dashboard')
  })

  it('defaults direct sign-in to the control dashboard', () => {
    expect(localReturnTo(undefined)).toBe('/dashboard')
  })
})

describe('authErrorMessage', () => {
  it('maps access errors to a useful message', () => {
    expect(authErrorMessage('access_denied')).toContain('不具备')
    expect(authErrorMessage(undefined)).toBe('')
  })
})
