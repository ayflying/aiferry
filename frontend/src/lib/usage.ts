import type { UsageLog } from '../api/types'

// channelCredentialReference 生成用量列表里「渠道 / 密钥」的标题：
// 运维给密钥填了备注时，备注比「#序号」更容易认出来，直接顶替序号显示
// （如「adesk 生产」）；没有备注则维持「渠道 #序号」的老样子。
// 序号仍然保留在接口数据里，只是不再出现在有备注的单元格上。
export function channelCredentialReference(
  channelName: string | undefined,
  index: number | undefined,
  remark?: string,
): string {
  const name = channelName?.trim() || '已删除渠道'
  const label = remark?.trim()
  if (label) return `${name} ${label}`
  return index && index > 0 ? `${name} #${index}` : `${name} #未记录`
}

// streamLabel 生成用量列表「流式」列的文案：上游尝试次数超过 1 时追加调用次数
// （如「流式 - 3」），让列表不展开详情就能看出这条请求经历过渠道切换或协议回退。
export function streamLabel(row: Pick<UsageLog, 'isStream' | 'attempts'>, full = false): string {
  const base = `${row.isStream ? '流式' : '非流式'}${full ? '响应' : ''}`
  return row.attempts > 1 ? `${base} - ${row.attempts}` : base
}
