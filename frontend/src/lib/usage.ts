export function channelCredentialLabel(index: number | undefined, prefix?: string): string {
  const keyPrefix = prefix?.trim()
  if (keyPrefix) return index && index > 0 ? `密钥 ${keyPrefix} · #${index}` : `密钥 ${keyPrefix}`
  return index && index > 0 ? `密钥 #${index}` : '未记录渠道密钥'
}

export function channelCredentialReference(channelName: string | undefined, index: number | undefined): string {
  const name = channelName?.trim() || '已删除渠道'
  return index && index > 0 ? `${name} #${index}` : `${name} #未记录`
}
