import type { ChannelModel, DiscoveredModel } from '../api/types'

export function compareModelNames(left: string, right: string) {
  return left.localeCompare(right, 'zh-CN', { sensitivity: 'base', numeric: true })
}

export function sortDiscoveredModels(models: DiscoveredModel[]) {
  return [...models].sort((left, right) => compareModelNames(left.name, right.name))
}

export function enabledChannelModels(models: ChannelModel[]) {
  return models
    .filter((model) => model.enabled === 1)
    .sort((left, right) => compareModelNames(left.publicName, right.publicName))
}

// 公开名批量写法的分隔符：半角逗号与全角逗号都算。
const publicNameSeparators = /[,，]/

// splitPublicModelNames 解析一行自定义公开名称：允许用逗号一次写出多个公开名，
// 空片段与重复片段丢弃，返回顺序即填写顺序。
export function splitPublicModelNames(value: string) {
  const seen = new Set<string>()
  const names: string[] = []
  for (const part of value.split(publicNameSeparators)) {
    const name = part.trim()
    if (!name || seen.has(name)) continue
    seen.add(name)
    names.push(name)
  }
  return names
}

// expandModelMappingRows 把对话框里逐行填写的映射展开成提交用的映射关系：
// 同一行的多个公开名各自生成一条「上游模型 → 公开名」关系，一个上游模型可批量挂多个名字。
export function expandModelMappingRows(rows: Array<{ upstreamName: string; publicName: string }>) {
  return rows.flatMap((row) => {
    const upstreamName = row.upstreamName.trim()
    return splitPublicModelNames(row.publicName).map((publicName) => ({ upstreamName, publicName }))
  })
}
