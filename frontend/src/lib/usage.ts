export function channelCredentialLabel(index: number | undefined): string {
  return index && index > 0 ? `密钥 #${index}` : '未记录渠道密钥'
}
