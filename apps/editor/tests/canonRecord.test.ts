import { describe, expect, it } from 'vitest'
import {
  canonCandidates,
  canonMessage,
  changeFrom,
  draftOf,
  typedValue
} from '../app/utils/canon/record'

const prop = (key: string, shape: string, immutable = false) => ({
  key,
  shape,
  doc: '',
  clauses: [],
  agent_may: true,
  immutable
})

const record = {
  entity: 'work_part',
  id: '7',
  version: 3,
  values: { label: '28', ordinal: 28, released: null, work_id: '5', kind: 'volume' },
  state: {},
  held: false
}

const properties = [
  prop('label', 'text'),
  prop('ordinal', 'ordinal'),
  prop('released', 'partial_date'),
  prop('kind', 'vocab'),
  prop('medium', 'vocab', true)
]

describe('canon record drafts', () => {
  it('sends only what changed, with the version it was edited against', () => {
    expect(changeFrom(record, properties, draftOf(record))).toBeNull()
    const draft = { ...draftOf(record), released: '2025-08-08', ordinal: '28.5' }
    expect(changeFrom(record, properties, draft)).toEqual({
      entity: 'work_part',
      id: '7',
      expect_version: 3,
      values: { released: '2025-08-08', ordinal: 28.5 }
    })
  })

  it('never sends an emptied field, an unreadable number or a fixed one', () => {
    const draft = { ...draftOf(record), label: ' ', ordinal: 'x', medium: 'manga' }
    expect(changeFrom(record, properties, draft)).toBeNull()
  })

  it('types values by their shape', () => {
    expect(typedValue(prop('m', 'bool'), true)).toBe(true)
    expect(typedValue(prop('n', 'integer'), '319')).toBe(319)
    expect(typedValue(prop('r', 'ref'), '12')).toBe('12')
  })
})

describe('canon problems', () => {
  it('explains a stale edit and lists what a new entity may already be', () => {
    expect(canonMessage({ data: { code: 'VERSION_CONFLICT', detail: 'x' } })).toContain('重新载入')
    expect(canonMessage({ data: { detail: 'd', errors: ['a: b'] } })).toBe('a: b')
    expect(canonMessage(null)).toContain('操作失败')
    const candidates = [{ token: 'canon:1', id: '1', label: 't' }]
    expect(canonCandidates({ data: { candidates } })).toEqual(candidates)
  })
})
