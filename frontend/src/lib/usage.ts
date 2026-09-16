import type { UsageLog } from '../api/types'

export function channelCredentialReference(channelName: string | undefined, index: number | undefined): string {
  const name = channelName?.trim() || '已删除渠道'
  return index && index > 0 ? `${name} #${index}` : `${name} #未记录`
}

// streamLabel 生成用量列表「流式」列的文案：上游尝试次数超过 1 时追加调用次数
// （如「流式 - 3」），让列表不展开详情就能看出这条请求经历过渠道切换或协议回退。
export function streamLabel(row: Pick<UsageLog, 'isStream' | 'attempts'>, full = false): string {
  const base = `${row.isStream ? '流式' : '非流式'}${full ? '响应' : ''}`
  return row.attempts > 1 ? `${base} - ${row.attempts}` : base
}
