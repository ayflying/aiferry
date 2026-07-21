import type { Channel } from '../api/types'

export const routeDisplayLimit = 6

export function isChannelRoutable(channel: Channel): boolean {
  return channel.status === 1 && !channel.autoDisabled && !channel.credentialsUnavailable
}

export function displayedRoutes(channels: Channel[]): Channel[] {
  return [...channels]
    .sort((left, right) => right.weight - left.weight || right.priority - left.priority || left.id - right.id)
    .slice(0, routeDisplayLimit)
}
