import { describe, expect, it } from 'vitest'
import {
  alignList,
  canonicalJson,
  diffLines,
  diffProse,
  diffText,
  hasChanges,
  naturalKeyMembers,
  summarizeList,
  valuesEqual
} from '../shared/utils/diff'
import {
  afterDecision,
  buildAmendment,
  decisionCounts,
  initialDecisions,
  keysToMerge,
  neighbourIds
} from '../app/utils/review/decision'
import {
  conflictingFields,
  latestRevision,
  orderPair,
  revertChanges
} from '../app/utils/review/history'
import { filterQueue } from '../app/utils/review/queue'
import { imageUrl, vocabularyLabel } from '../app/utils/review/describe'

const join = (segments: { op: string; text: string }[], op: string) =>
  segments
    .filter((segment) => segment.op === op || segment.op === 'same')
    .map((segment) => segment.text)
    .join('')

describe('text diff', () => {
  it('reconstructs both sides', () => {
    const before = 'Summer Pockets 是 Key 的作品'
    const after = 'Summer Pockets REFLECTION BLUE 是 Key 的作品'
    const segments = diffText(before, after)
    expect(join(segments, 'del')).toBe(before)
    expect(join(segments, 'add')).toBe(after)
    expect(
      segments
        .filter((s) => s.op === 'add')
        .map((s) => s.text)
        .join('')
        .trim()
    ).toBe('REFLECTION BLUE')
    expect(segments.some((s) => s.op === 'del')).toBe(false)
  })

  it('diffs CJK text by character', () => {
    const segments = diffText('夏日口袋', '夏日口袋反射蓝')
    expect(segments).toEqual([
      { op: 'same', text: '夏日口袋' },
      { op: 'add', text: '反射蓝' }
    ])
  })

  it('reports no change for equal text', () => {
    expect(hasChanges(diffText('a b', 'a b'))).toBe(false)
  })

  it('diffs by line', () => {
    const segments = diffLines('一\n二\n三\n', '一\n贰\n三\n')
    expect(segments).toEqual([
      { op: 'same', text: '一\n' },
      { op: 'del', text: '二\n' },
      { op: 'add', text: '贰\n' },
      { op: 'same', text: '三\n' }
    ])
  })

  it('falls back to a whole replacement when the table would be huge', () => {
    const before = Array.from({ length: 800 }, (_, i) => `a${i} `).join('')
    const after = Array.from({ length: 800 }, (_, i) => `b${i} `).join('')
    const segments = diffText(before, after)
    expect(join(segments, 'del')).toBe(before)
    expect(join(segments, 'add')).toBe(after)
  })
})

describe('list diff', () => {
  it('classifies added, removed, kept and moved refs', () => {
    const entries = alignList(['1', '2', '3'], ['3', '1', '4'])
    const byValue = Object.fromEntries(
      entries.map((e) => [String(e.after ?? e.before), e.status])
    )
    expect(byValue).toEqual({
      '1': 'kept',
      '3': 'moved',
      '4': 'added',
      '2': 'removed'
    })
    expect(summarizeList(entries)).toEqual({
      added: 1,
      removed: 1,
      changed: 0,
      moved: 1
    })
  })

  it('treats an edited row with the same natural key as changed', () => {
    const members = [
      { key: 'character_id', type: 'ref' },
      { key: 'kind', type: 'int' },
      { key: 'spoiler', type: 'int' }
    ]
    const keys = naturalKeyMembers('catalog.work.roster', members)
    expect(keys).toEqual(['character_id'])
    const entries = alignList(
      [{ character_id: '7', kind: 0, spoiler: 0 }],
      [{ character_id: '7', kind: 1, spoiler: 0 }],
      keys
    )
    expect(entries).toHaveLength(1)
    expect(entries[0]!.status).toBe('changed')
  })

  it('falls back to ref and lang members for unknown fields', () => {
    expect(
      naturalKeyMembers('catalog.thing.items', [
        { key: 'lang', type: 'enum' },
        { key: 'text', type: 'text' },
        { key: 'target', type: 'ref' }
      ])
    ).toEqual(['lang', 'target'])
  })

  it('keeps duplicates apart', () => {
    const entries = alignList(['a', 'a'], ['a'])
    expect(entries.map((e) => e.status).sort()).toEqual(['kept', 'removed'])
  })
})

describe('field decisions', () => {
  const effective = {
    'catalog.work.display_name': 'B',
    'catalog.work.olang': 'ja',
    'catalog.work.tag_ids': ['1', '2']
  }

  it('starts with every field accepted and needs no amendment', () => {
    const decisions = initialDecisions(effective)
    expect(buildAmendment(effective, decisions)).toBeNull()
    expect(keysToMerge(effective, decisions)).toHaveLength(3)
  })

  it('builds set and unset from amend and reject', () => {
    const decisions = {
      ...initialDecisions(effective),
      'catalog.work.display_name': { verdict: 'amend' as const, value: 'C' },
      'catalog.work.olang': { verdict: 'reject' as const }
    }
    expect(buildAmendment(effective, decisions)).toEqual({
      set: { 'catalog.work.display_name': 'C' },
      unset: ['catalog.work.olang']
    })
    expect(keysToMerge(effective, decisions)).toEqual([
      'catalog.work.display_name',
      'catalog.work.tag_ids'
    ])
    expect(decisionCounts(decisions)).toEqual({
      accept: 1,
      reject: 1,
      amend: 1
    })
  })

  it('drops an amendment equal to the proposed value', () => {
    const decisions = {
      ...initialDecisions(effective),
      'catalog.work.tag_ids': { verdict: 'amend' as const, value: ['1', '2'] }
    }
    expect(buildAmendment(effective, decisions)).toBeNull()
  })

  it('leaves nothing to merge when every field is rejected', () => {
    const decisions = Object.fromEntries(
      Object.keys(effective).map((key) => [key, { verdict: 'reject' as const }])
    )
    expect(keysToMerge(effective, decisions)).toEqual([])
  })
})

describe('queue navigation', () => {
  it('finds neighbours and the next item after a decision', () => {
    expect(neighbourIds(['1', '2', '3'], '2')).toEqual({
      previous: '1',
      next: '3'
    })
    expect(afterDecision(['1', '2', '3'], '3')).toEqual({
      remaining: ['1', '2'],
      target: '2'
    })
    expect(afterDecision(['5'], '5')).toEqual({ remaining: [], target: null })
  })

  it('filters by site, age, field and conflict', () => {
    const now = Date.parse('2026-10-20T00:00:00Z')
    const items = [
      { id: '1', site: 'kungal', created_at: '2026-10-19T12:00:00Z' },
      { id: '2', site: 'letmoe', created_at: '2026-10-01T00:00:00Z' },
      { id: '3', site: 'kungal', created_at: '2026-09-01T00:00:00Z' }
    ]
    const analyses = {
      '1': { fields: ['catalog.work.titles'], conflicts: {} },
      '3': {
        fields: ['catalog.work.intros'],
        conflicts: { 'catalog.work.intros': 9 }
      }
    }
    const base = { site: '', age: '', field: '', conflictOnly: false }
    expect(
      filterQueue(items, { ...base, site: 'kungal' }, {}, now).map((i) => i.id)
    ).toEqual(['1', '3'])
    expect(
      filterQueue(items, { ...base, age: 'gt7' }, {}, now).map((i) => i.id)
    ).toEqual(['2', '3'])
    expect(
      filterQueue(items, { ...base, age: 'lt1' }, {}, now).map((i) => i.id)
    ).toEqual(['1'])
    expect(
      filterQueue(
        items,
        { ...base, field: 'catalog.work.titles' },
        analyses,
        now
      ).map((i) => i.id)
    ).toEqual(['1'])
    expect(
      filterQueue(items, { ...base, conflictOnly: true }, analyses, now).map(
        (i) => i.id
      )
    ).toEqual(['3'])
  })
})

describe('history helpers', () => {
  const revisions = [
    { id: '10', seq: 1, changed_fields: ['catalog.work.titles'] },
    { id: '11', seq: 2, changed_fields: ['catalog.work.intros'] },
    {
      id: '12',
      seq: 3,
      changed_fields: ['catalog.work.titles.suppressed', 'catalog.work.olang']
    }
  ]

  it('flags proposal fields touched after its base revision', () => {
    expect(
      conflictingFields(1, revisions, [
        'catalog.work.titles',
        'catalog.work.intros'
      ])
    ).toEqual({ 'catalog.work.intros': 2 })
    expect(conflictingFields(3, revisions, ['catalog.work.olang'])).toEqual({})
    expect(
      conflictingFields(2, revisions, [
        'catalog.work.titles.suppressed',
        'catalog.work.olang'
      ])
    ).toEqual({ 'catalog.work.titles.suppressed': 3, 'catalog.work.olang': 3 })
  })

  it('picks the latest revision and orders a pair', () => {
    expect(latestRevision(revisions)?.id).toBe('12')
    expect(latestRevision([])).toBeNull()
    expect(orderPair(revisions[2]!, revisions[0]!)).toEqual({
      older: revisions[0],
      newer: revisions[2]
    })
  })

  it('turns a latest-vs-target diff into what a revert restores', () => {
    expect(
      revertChanges([{ key: 'catalog.work.olang', from: 'ja', to: 'en' }])
    ).toEqual([{ key: 'catalog.work.olang', current: 'en', restored: 'ja' }])
  })
})

describe('values', () => {
  it('compares values regardless of key order', () => {
    expect(
      valuesEqual(
        { a: 1, b: [1, { c: 2, d: 3 }] },
        { b: [1, { d: 3, c: 2 }], a: 1 }
      )
    ).toBe(true)
    expect(canonicalJson(undefined)).toBe('null')
    expect(valuesEqual([1, 2], [2, 1])).toBe(false)
  })

  it('labels int-coded and token vocabularies', () => {
    const vocabulary = {
      values: [{ value: 'main', display_name: 'Main' }, { value: 'secondary' }],
      labels: { main: '主要角色' }
    }
    expect(vocabularyLabel(0, vocabulary, 'int', 0)).toBe('主要角色')
    expect(vocabularyLabel(1, vocabulary, 'int', 0)).toBe('secondary')
    expect(vocabularyLabel('main', vocabulary, 'token')).toBe('主要角色')
    expect(vocabularyLabel(null, vocabulary, 'token')).toBe('—')
    expect(vocabularyLabel(9, vocabulary, 'int', 0)).toBe('9')
  })

  it('builds image URLs from a hash', () => {
    expect(imageUrl('e3b7abcd')).toBe(
      'https://image.kungal.iloveren.link/e3/b7/e3b7abcd.webp'
    )
    expect(imageUrl('ab')).toBe('')
    expect(imageUrl(null)).toBe('')
  })
})

describe('list pairing', () => {
  const titleMembers = [
    { key: 'lang', type: 'enum' },
    { key: 'title', type: 'text' },
    { key: 'kind', type: 'int' },
    { key: 'latin', type: 'text' }
  ]

  it('reads a renamed official title as one change and leaves aliases alone', () => {
    const keys = naturalKeyMembers('catalog.work.titles', titleMembers)
    const before = [
      { lang: 'ja', title: 'Summer Pockets', kind: 0, latin: null },
      { lang: null, title: 'サマポケ', kind: 1, latin: null },
      { lang: null, title: 'SP', kind: 1, latin: null }
    ]
    const after = [
      {
        lang: 'ja',
        title: 'Summer Pockets REFLECTION BLUE',
        kind: 0,
        latin: null
      },
      { lang: null, title: 'サマポケ', kind: 1, latin: null },
      { lang: null, title: 'SP', kind: 1, latin: null },
      { lang: null, title: 'SPRB', kind: 2, latin: null }
    ]
    const entries = alignList(before, after, keys)
    expect(entries.map((e) => e.status)).toEqual([
      'changed',
      'kept',
      'kept',
      'added'
    ])
    expect(summarizeList(entries)).toEqual({
      added: 1,
      removed: 0,
      changed: 1,
      moved: 0
    })
  })

  it('prefers exact matches before pairing by key', () => {
    const entries = alignList(
      [
        { character_id: '1', kind: 0 },
        { character_id: '1', kind: 1 }
      ],
      [{ character_id: '1', kind: 1 }],
      ['character_id']
    )
    expect(entries.map((e) => e.status)).toEqual(['kept', 'removed'])
  })
})

describe('prose diff', () => {
  it('diffs short prose by character and long prose by line', () => {
    expect(diffProse('一二三', '一二四三')).toEqual([
      { op: 'same', text: '一二' },
      { op: 'add', text: '四' },
      { op: 'same', text: '三' }
    ])
    const long = 'x'.repeat(4000)
    const segments = diffProse(`${long}\na\n`, `${long}\nb\n`)
    expect(segments.map((s) => s.op)).toEqual(['same', 'del', 'add'])
  })
})
