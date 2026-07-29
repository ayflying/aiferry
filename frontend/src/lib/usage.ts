export function channelCredentialReference(channelName: string | undefined, index: number | undefined): string {
  const name = channelName?.trim() || '已删除渠道'
  return index && index > 0 ? `${name} #${index}` : `${name} #未记录`
}
