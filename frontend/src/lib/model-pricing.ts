import type { ModelBillingMode, PublicModel } from '../api/types'

type TokenPriceField = Pick<PublicModel,
  | 'inputPrice'
  | 'cachedInputPrice'
  | 'cacheWritePrice'
  | 'outputPrice'
  | 'imageInputPrice'
  | 'audioInputPrice'
  | 'audioOutputPrice'>

export interface TokenPriceItem {
  label: string
  value: number
}

const tokenPriceFields: Array<{ label: string; field: keyof TokenPriceField }> = [
  { label: '输入', field: 'inputPrice' },
  { label: '缓存读', field: 'cachedInputPrice' },
  { label: '缓存写', field: 'cacheWritePrice' },
  { label: '输出', field: 'outputPrice' },
  { label: '图像输入', field: 'imageInputPrice' },
  { label: '音频输入', field: 'audioInputPrice' },
  { label: '音频输出', field: 'audioOutputPrice' },
]

export function configuredTokenPriceItems(model: TokenPriceField): TokenPriceItem[] {
  return tokenPriceFields.flatMap(({ label, field }) => {
    const value = model[field]
    return typeof value === 'number' && Number.isFinite(value) ? [{ label, value }] : []
  })
}

export function formatModelPrice(value: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 6 }).format(value)
}

export function modelBillingModeLabel(mode: ModelBillingMode): string {
  switch (mode) {
    case 'request': return '按请求'
    case 'rules': return '高级规则'
    default: return '按 Token'
  }
}
