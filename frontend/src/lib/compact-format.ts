// 只压缩展示值，不改变统计与计费数据；边界先按两位小数舍入，避免出现 1000K、60秒。
function formatScaled(value: number | null | undefined, scales: readonly number[], units: readonly string[]): string {
  if (value == null || !Number.isFinite(value)) return '—'
  let index = 0
  while (index < scales.length - 1 && Math.abs(value) >= scales[index + 1]!) index++
  if (index < scales.length - 1 && Number((Math.abs(value) / scales[index]!).toFixed(2)) >= scales[index + 1]! / scales[index]!) index++
  return `${Number((value / scales[index]!).toFixed(2))}${units[index]}`
}

/** 十进制单位：K=千、M=百万、B=十亿、T=万亿。 */
export function formatCompactTokens(value?: number | null): string {
  return formatScaled(value, [1, 1e3, 1e6, 1e9, 1e12], ['', 'K', 'M', 'B', 'T'])
}

/** 输入为毫秒，自动使用最合适的时间单位，最多保留两位小数。 */
export function formatCompactDuration(value?: number | null): string {
  return formatScaled(value, [1, 1000, 60_000, 3_600_000, 86_400_000], [' ms', ' 秒', ' 分钟', ' 小时', ' 天'])
}
