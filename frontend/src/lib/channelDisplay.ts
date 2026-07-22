import type { Channel } from '../api/types'

type ChannelAvailability = Pick<Channel, 'status' | 'autoDisabled' | 'credentialsUnavailable'>
type ChannelReference = Pick<Channel, 'id' | 'name'>

export function isChannelEnabled(channel: ChannelAvailability) {
  return channel.status === 1 && !channel.autoDisabled && !channel.credentialsUnavailable
}

export function channelStatusLabel(channel: ChannelAvailability) {
  if (channel.autoDisabled) return '渠道自动禁用'
  if (channel.status !== 1) return '手动停用'
  if (channel.credentialsUnavailable) return '所有密钥不可用'
  return '启用'
}

export function channelNameForID(channels: ChannelReference[], channelID: number) {
  return channels.find((channel) => channel.id === channelID)?.name || `#${channelID}`
}
