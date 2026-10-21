import { describe, expect, it } from 'vitest'
import type { CatalogSchemaField } from '../shared/types/catalog'
import { threeWayMerge } from '../shared/utils/threeWay'
import {
  basesFromDiff,
  conflictBaseQuery
} from '../app/utils/review/conflictBase'

const field = (spec: Record<string, unknown>) =>
  spec as unknown as CatalogSchemaField

const displayName = field({
  key: 'catalog.work.display_name',
  field_type: 'text'
})
const tagIds = field({
  key: 'catalog.work.tag_ids',
  field_type: 'list',
  element: { type: 'ref' }
})
const suppressedTags = field({
  key: 'catalog.work.tag_ids.suppressed',
  field_type: 'list',
  element: { type: 'text' }
})
const intros = field({
  key: 'catalog.work.intros',
  field_type: 'list',
  element: {
    type: 'object',
    members: [
      { key: 'lang', type: 'text' },
      { key: 'intro', type: 'text' }
    ]
  }
})
const roster = field({
  key: 'catalog.work.roster',
  field_type: 'list',
  element: {
    type: 'object',
    members: [
      { key: 'character_id', type: 'ref' },
      { key: 'kind', type: 'int' },
      { key: 'spoiler', type: 'int' }
    ]
  }
})

describe('threeWayMerge: scalars', () => {
  it('takes the proposal when nobody else touched the value', () => {
    expect(threeWayMerge(displayName, 'A', 'A', 'B')).toEqual({
      clean: true,
      value: 'B',
      overlaps: []
    })
  })

  it('keeps the current value when the proposal no longer changes anything', () => {
    expect(threeWayMerge(displayName, 'A', 'C', 'A').value).toBe('C')
    expect(threeWayMerge(displayName, 'A', 'B', 'B').clean).toBe(true)
  })

  it('never auto-merges two different edits of one scalar', () => {
    const result = threeWayMerge(displayName, 'A', 'B', 'C')
    expect(result.clean).toBe(false)
    expect(result.value).toBe('B')
    expect(result.overlaps).toHaveLength(1)
  })
})

describe('threeWayMerge: sets', () => {
  it('applies the proposal’s additions and removals onto the current list', () => {
    const result = threeWayMerge(
      tagIds,
      ['1', '2', '3'],
      ['1', '2', '3', '9'],
      ['2', '3', '4']
    )
    expect(result.clean).toBe(true)
    expect(result.value).toEqual(['2', '3', '9', '4'])
  })

  it('does not duplicate an element both sides added', () => {
    const result = threeWayMerge(tagIds, ['1'], ['1', '5'], ['1', '5'])
    expect(result.value).toEqual(['1', '5'])
  })

  it('keeps hidden-row identities sorted by code point and unique', () => {
    const result = threeWayMerge(
      suppressedTags,
      ['tag:1'],
      ['tag:1', 'tag:30'],
      ['tag:1', 'tag:2']
    )
    expect(result.clean).toBe(true)
    expect(result.value).toEqual(['tag:1', 'tag:2', 'tag:30'])
  })

  it('lets the proposal un-hide a row while the current side hid another', () => {
    const result = threeWayMerge(
      suppressedTags,
      ['tag:1', 'tag:2'],
      ['tag:1', 'tag:2', 'tag:3'],
      ['tag:2']
    )
    expect(result.value).toEqual(['tag:2', 'tag:3'])
  })
})

describe('threeWayMerge: rows', () => {
  const ja = { lang: 'ja', intro: '日本語の紹介' }
  const en = { lang: 'en', intro: 'English intro' }

  it('merges edits to different rows', () => {
    const result = threeWayMerge(
      intros,
      [ja, en],
      [{ ...ja, intro: '新しい紹介' }, en],
      [ja, { ...en, intro: 'Better intro' }]
    )
    expect(result.clean).toBe(true)
    expect(result.value).toEqual([
      { lang: 'ja', intro: '新しい紹介' },
      { lang: 'en', intro: 'Better intro' }
    ])
  })

  it('appends rows the proposal added after the current order', () => {
    const zh = { lang: 'zh-Hans', intro: '中文简介' }
    const result = threeWayMerge(intros, [ja], [en, ja], [ja, zh])
    expect(result.clean).toBe(true)
    expect(result.value).toEqual([en, ja, zh])
  })

  it('flags a row both sides rewrote differently', () => {
    const result = threeWayMerge(
      intros,
      [ja],
      [{ ...ja, intro: 'A' }],
      [{ ...ja, intro: 'B' }]
    )
    expect(result.clean).toBe(false)
    expect(result.overlaps).toEqual([
      expect.objectContaining({ identity: 'ja', members: ['intro'] })
    ])
    expect(result.value).toEqual([{ lang: 'ja', intro: 'A' }])
  })

  it('merges different members of the same row', () => {
    const base = [{ character_id: 7, kind: 1, spoiler: 0 }]
    const result = threeWayMerge(
      roster,
      base,
      [{ character_id: 7, kind: 2, spoiler: 0 }],
      [{ character_id: 7, kind: 1, spoiler: 2 }]
    )
    expect(result.clean).toBe(true)
    expect(result.value).toEqual([{ character_id: 7, kind: 2, spoiler: 2 }])
  })

  it('flags a row the proposal removed but the current side changed', () => {
    const result = threeWayMerge(
      roster,
      [{ character_id: 7, kind: 1, spoiler: 0 }],
      [{ character_id: 7, kind: 2, spoiler: 0 }],
      []
    )
    expect(result.clean).toBe(false)
    expect(result.overlaps[0]).toMatchObject({ identity: '7' })
  })

  it('flags a row the proposal changed but the current side removed', () => {
    const result = threeWayMerge(
      roster,
      [{ character_id: 7, kind: 1, spoiler: 0 }],
      [],
      [{ character_id: 7, kind: 2, spoiler: 0 }]
    )
    expect(result.clean).toBe(false)
    expect(result.value).toEqual([])
  })

  it('removes a row the proposal removed when nobody else changed it', () => {
    const keep = { character_id: 8, kind: 1, spoiler: 0 }
    const result = threeWayMerge(
      roster,
      [{ character_id: 7, kind: 1, spoiler: 0 }, keep],
      [
        { character_id: 7, kind: 1, spoiler: 0 },
        keep,
        { character_id: 9, kind: 3, spoiler: 0 }
      ],
      [keep]
    )
    expect(result.clean).toBe(true)
    expect(result.value).toEqual([
      keep,
      { character_id: 9, kind: 3, spoiler: 0 }
    ])
  })

  it('flags the same row added by both sides with different content', () => {
    const result = threeWayMerge(
      roster,
      [],
      [{ character_id: 7, kind: 1, spoiler: 0 }],
      [{ character_id: 7, kind: 2, spoiler: 0 }]
    )
    expect(result.clean).toBe(false)
    expect(result.overlaps[0]?.members).toEqual(['kind'])
  })

  it('treats a missing base like an empty list', () => {
    const result = threeWayMerge(intros, undefined, [en], [ja])
    expect(result.clean).toBe(true)
    expect(result.value).toEqual([en, ja])
  })
})

describe('conflict bases', () => {
  const revisions = [
    { id: '10', seq: 1, changed_fields: ['a'] },
    { id: '11', seq: 2, changed_fields: ['b'] },
    { id: '12', seq: 3, changed_fields: ['a'] }
  ]

  it('diffs the latest revision against the proposal’s base revision', () => {
    expect(conflictBaseQuery(1, revisions)).toEqual({
      revisionId: '12',
      diffBase: '10'
    })
  })

  it('diffs against the pre-edit baseline when the proposal predates every revision', () => {
    expect(conflictBaseQuery(0, revisions)).toEqual({
      revisionId: '12',
      diffBase: 'baseline'
    })
  })

  it('gives up when the base revision is outside the fetched history', () => {
    expect(conflictBaseQuery(1, revisions.slice(1))).toBeNull()
    expect(conflictBaseQuery(3, revisions)).toBeNull()
  })

  it('takes the base from the diff and falls back to the current value', () => {
    expect(
      basesFromDiff([{ key: 'a', from: 'old', to: 'new' }], ['a', 'b'], {
        a: 'new',
        b: 'same'
      })
    ).toEqual({ a: 'old', b: 'same' })
  })
})
