import { describe, expect, it } from 'vitest'
import { priceSourceLocation } from './priceSource'
import type { PriceSource } from '../api/types'

function source(baseUrl: string, path: string): PriceSource {
  return {
    id: 1,
    name: '源',
    code: 'source',
    config: { baseUrl, pricing: { adapter: 'json', path } } as PriceSource['config'],
    status: 1,
    builtIn: 0,
    createdAt: '',
    updatedAt: '',
  }
}

describe('价格源地址展示', () => {
  it('相对路径拼接到根地址之后', () => {
    expect(priceSourceLocation(source('https://prices.example.com', '/v1/prices'))).toBe('https://prices.example.com/v1/prices')
  })

  it('完整地址直接展示，不再拼根地址', () => {
    expect(priceSourceLocation(source('https://prices.example.com', 'https://vendor.example.com/api/prices'))).toBe('https://vendor.example.com/api/prices')
    expect(priceSourceLocation(source('https://prices.example.com', 'HTTPS://vendor.example.com/api/prices'))).toBe('HTTPS://vendor.example.com/api/prices')
  })

  it('配置字段缺失时不抛错', () => {
    const broken = { id: 2, name: 'x', code: 'y', status: 1, builtIn: 0, createdAt: '', updatedAt: '' } as unknown as PriceSource
    expect(priceSourceLocation(broken)).toBe('')
  })
})
