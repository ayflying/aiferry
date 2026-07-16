import { describe, expect, it } from 'vitest'
import type { ChannelModel, DiscoveredModel } from '../api/types'
import { enabledChannelModels, sortDiscoveredModels } from './models'

describe('model lists', () => {
  it('sorts discovered models by name without mutating the response', () => {
    const input: DiscoveredModel[] = [
      { name: 'gpt-10', selected: false },
      { name: 'gpt-2', selected: true },
      { name: 'GPT-1', selected: false },
    ]

    expect(sortDiscoveredModels(input).map((item) => item.name)).toEqual(['GPT-1', 'gpt-2', 'gpt-10'])
    expect(input[0].name).toBe('gpt-10')
  })

  it('keeps only selected channel models for testing and sorts them', () => {
    const model = (id: number, publicName: string, enabled: number) => ({ id, publicName, enabled }) as ChannelModel
    const result = enabledChannelModels([
      model(1, 'zeta', 1),
      model(2, 'hidden', 0),
      model(3, 'alpha', 1),
    ])

    expect(result.map((item) => item.publicName)).toEqual(['alpha', 'zeta'])
  })
})
