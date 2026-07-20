export function formatIPLocation(location?: string) {
  return location?.split('/').map((part) => part.trim()).filter(Boolean).join(' - ') || '归属地未识别'
}
