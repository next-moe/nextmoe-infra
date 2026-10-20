import { describe, expect, it } from 'vitest'
import type { CatalogEditContextRow } from '../shared/types/catalog'
import {
  creditKeyClash,
  groupRows,
  hiddenRows,
  humanRowKey,
  idOfContextValue,
  matchesFilter,
  mergeIdRows,
  mergeRelatedRows,
  relationPhrase,
  sourceLabel,
  upstreamRowKey,
  visibleRows
} from '../app/utils/relatedRows'
import { rowIdentity } from '../shared/utils/editModel'

const row = (
  partial: Partial<CatalogEditContextRow> & { value: unknown }
): CatalogEditContextRow =>
  ({
    source: { key: 'vndb', display: 'VNDB' },
    human_lane: false,
    identity: null,
    suppressed: false,
    present: true,
    machine_translated: false,
    field_provenance: null,
    ...partial
  }) as CatalogEditContextRow

describe('mergeRelatedRows', () => {
  const fieldKey = 'catalog.work.credits'
  const upstream = [
    row({
      identity: 'credit:1:422:8086',
      value: { role_id: 1, credit_name_id: 422, character_id: 8086 }
    }),
    row({
      identity: 'credit:2:9:0',
      source: { key: 'import:staff-bangumi', display: 'import:staff-bangumi' },
      value: { role_id: 2, credit_name_id: 9, character_id: null }
    }),
    row({
      identity: 'credit:3:5:0',
      human_lane: true,
      value: { role_id: 3, credit_name_id: 5 }
    }),
    row({
      identity: 'credit:4:5:0',
      present: false,
      value: { role_id: 4, credit_name_id: 5 }
    })
  ]

  it('puts the human lane first, then visible upstream rows', () => {
    const base = [{ role_id: 3, credit_name_id: 5, character_id: null }]
    const human = [
      { role_id: 3, credit_name_id: 5, character_id: null },
      { role_id: 7, credit_name_id: 8, character_id: null }
    ]
    const merged = mergeRelatedRows({
      fieldKey,
      human,
      base,
      context: upstream,
      suppressed: ['credit:2:9:0']
    })
    expect(merged.map((entry) => entry.key)).toEqual([
      humanRowKey(0),
      humanRowKey(1),
      upstreamRowKey('credit:1:422:8086'),
      upstreamRowKey('credit:2:9:0')
    ])
    expect(merged.map((entry) => entry.state)).toEqual([
      null,
      'added',
      null,
      'hidden'
    ])
    expect(merged[3]!.source).toBe('Bangumi')
    expect(visibleRows(merged)).toHaveLength(3)
    expect(hiddenRows(merged)).toHaveLength(1)
  })

  it('marks an edited human row as changed and keeps its index key', () => {
    const base = [
      { role_id: 3, credit_name_id: 5, character_id: null, note: '' }
    ]
    const human = [
      { role_id: 3, credit_name_id: 5, character_id: null, note: 'FD' }
    ]
    const [first] = mergeRelatedRows({
      fieldKey,
      human,
      base,
      context: [],
      suppressed: []
    })
    expect(first!.state).toBe('changed')
    expect(first!.key).toBe('h:0')
    expect(first!.index).toBe(0)
  })
})

describe('mergeIdRows', () => {
  it('skips upstream rows that repeat a human id and reads ids out of objects', () => {
    const merged = mergeIdRows({
      human: [13, 99],
      base: [13],
      context: [
        row({ identity: 'engine:13', value: 13 }),
        row({ identity: 'engine:20', value: 20 }),
        row({
          identity: 'series:1:x',
          value: { series_id: 725, position: 1, kind: 1 }
        })
      ],
      suppressed: ['engine:20']
    })
    expect(
      merged.map((entry) => [entry.id, entry.origin, entry.state])
    ).toEqual([
      ['13', 'human', null],
      ['99', 'human', 'added'],
      ['20', 'upstream', 'hidden'],
      ['725', 'upstream', null]
    ])
  })

  it('reads the id of a context value', () => {
    expect(idOfContextValue(7)).toBe('7')
    expect(idOfContextValue('ps2')).toBe('ps2')
    expect(idOfContextValue({ engine_id: 3 })).toBe('3')
    expect(idOfContextValue(null)).toBe('')
  })
})

describe('helpers', () => {
  it('names sources the way people know them', () => {
    expect(sourceLabel({ key: 'import:character-roster-bangumi' })).toBe(
      'Bangumi'
    )
    expect(sourceLabel({ key: 'erogamescape', display: 'ErogameScape' })).toBe(
      'EGS'
    )
    expect(sourceLabel({ key: 'curated', display: 'Curated' })).toBe('人工')
    expect(sourceLabel({ key: 'other', display: 'Other' })).toBe('Other')
  })

  it('phrases a relation from this work, ignoring reverse on symmetric ones', () => {
    expect(relationPhrase('sequel_of', false)).toBe('是…的续作')
    expect(relationPhrase('sequel_of', true)).toBe('有续作')
    expect(relationPhrase('same_series', true)).toBe('同系列')
    expect(relationPhrase('unknown_kind', false)).toBe('unknown_kind')
  })

  it('detects a human credit that repeats an upstream one', () => {
    const merged = mergeRelatedRows({
      fieldKey: 'catalog.work.credits',
      human: [],
      base: [],
      context: [
        row({
          identity: 'credit:1:422:8086',
          value: { role_id: 1, credit_name_id: 422, character_id: 8086 }
        })
      ],
      suppressed: []
    })
    expect(
      creditKeyClash(
        { role_id: '1', credit_name_id: 422, character_id: 8086 },
        merged
      )
    ).toBe(true)
    expect(
      creditKeyClash(
        { role_id: 1, credit_name_id: 422, character_id: null },
        merged
      )
    ).toBe(false)
  })

  it('groups in the given order and filters case-insensitively', () => {
    const rows = mergeIdRows({
      human: [1, 2, 3],
      base: [1, 2, 3],
      context: [],
      suppressed: []
    }).map((entry) => ({ ...entry, value: entry, index: entry.index }))
    const groups = groupRows(
      rows as never,
      (entry: { id: string }) => (entry.id === '2' ? 'b' : 'a'),
      ['b', 'a']
    )
    expect(groups.map((group) => group.key)).toEqual(['b', 'a'])
    expect(matchesFilter('KEY', 'Key Visual Arts')).toBe(true)
    expect(matchesFilter('', 'anything')).toBe(true)
    expect(matchesFilter('x', null, undefined)).toBe(false)
  })

  it('gives work relations to different works different identities', () => {
    const fieldKey = 'catalog.work.relations'
    expect(
      rowIdentity(fieldKey, {
        relation: 'sequel_of',
        work_id: 1,
        reverse: true
      })
    ).not.toBe(
      rowIdentity(fieldKey, {
        relation: 'sequel_of',
        work_id: 2,
        reverse: true
      })
    )
    expect(rowIdentity(fieldKey, { relation: 'imprint_of', label_id: 9 })).toBe(
      'imprint_of:9'
    )
  })
})
