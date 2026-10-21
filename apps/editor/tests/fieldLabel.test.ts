import { describe, expect, it } from 'vitest'
import { fieldLabel } from '../app/utils/format'

describe('fieldLabel', () => {
  it('names every registered field the way the edit form does', () => {
    expect(fieldLabel('catalog.work.relations')).toBe('关联作品')
    expect(fieldLabel('catalog.label.relations')).not.toBe('relations')
  })

  it('marks a hidden-rows companion', () => {
    expect(fieldLabel('catalog.work.relations.suppressed')).toBe(
      '关联作品(屏蔽)'
    )
  })

  it('falls back to the short table, then the bare key, for legacy keys', () => {
    expect(fieldLabel('galgame.game.tag_ids')).toBe('标签')
    expect(fieldLabel('galgame.game.bid')).toBe('bid')
  })
})
