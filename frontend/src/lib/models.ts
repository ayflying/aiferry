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
