import { describe, expect, it } from 'vitest'
import type { ChannelModel, DiscoveredModel } from '../api/types'
import { enabledChannelModels, expandModelMappingRows, sortDiscoveredModels, splitPublicModelNames } from './models'

describe('model lists', () => {
  it('sorts discovered models by name without mutating the response', () => {
    const input: DiscoveredModel[] = [
      { name: 'gpt-10', publicName: 'gpt-10', selected: false },
      { name: 'gpt-2', publicName: 'gpt-2', selected: true },
      { name: 'GPT-1', publicName: 'GPT-1', selected: false },
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

describe('model mapping public names', () => {
  it('splits comma separated public names and drops blank or repeated parts', () => {
    expect(splitPublicModelNames(' space-bunny, free ，space-bunny ,, ')).toEqual(['space-bunny', 'free'])
    expect(splitPublicModelNames(' , ， ')).toEqual([])
    expect(splitPublicModelNames('mimo-v2.6-flash')).toEqual(['mimo-v2.6-flash'])
  })

  it('expands one row into several mappings of the same upstream model', () => {
    const rows = [
      { upstreamName: ' space-bunny-free ', publicName: 'space-bunny,free' },
      { upstreamName: 'mimo-v2.6-flash-free', publicName: 'mimo-v2.6-flash' },
    ]

    expect(expandModelMappingRows(rows)).toEqual([
      { upstreamName: 'space-bunny-free', publicName: 'space-bunny' },
      { upstreamName: 'space-bunny-free', publicName: 'free' },
      { upstreamName: 'mimo-v2.6-flash-free', publicName: 'mimo-v2.6-flash' },
    ])
  })

  it('keeps an incomplete row out of the expanded result so the caller can report it', () => {
    expect(expandModelMappingRows([{ upstreamName: 'gpt-5', publicName: ' , ' }])).toEqual([])
  })
})
