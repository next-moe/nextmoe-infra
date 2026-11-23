import { describe, expect, it } from 'vitest'
import { citableRefs, suggestionLines, useIDsFrom } from '../app/utils/canon/todo'

const row = {
  kind: 'changeset' as const,
  changeset: {
    lead_id: 'l',
    entities: [
      { ref: 'person.x', entity: 'person', properties: {} },
      {
        ref: 'alias.x',
        entity: 'person_alias',
        properties: {
          person_id: { value: 'ref:person.x' },
          name: { value: '川原 礫', tier: 'registry' }
        }
      },
      {
        ref: 'credit.0',
        entity: 'credit',
        properties: { work_id: { value: 42 }, role: { value: 'author' } }
      }
    ]
  }
}

describe('canon todo suggestion', () => {
  it('lists every entity with its values', () => {
    const lines = suggestionLines(row)
    expect(lines.map((l) => l.ref)).toEqual(['person.x', 'alias.x', 'credit.0'])
    expect(lines[1]?.values).toContainEqual({ key: 'person_id', value: '→ person.x', tier: undefined })
    expect(lines[2]?.values).toContainEqual({ key: 'work_id', value: '42', tier: undefined })
  })

  it('reads a single candidate', () => {
    const lines = suggestionLines({
      kind: 'candidate',
      candidate: { target: { entity: 'release', id: 9 }, property: 'released', value: '2025-08-08', tier: 'registry' }
    })
    expect(lines).toEqual([
      { ref: 'release 9', entity: 'release', values: [{ key: 'released', value: '2025-08-08', tier: 'registry' }] }
    ])
  })

  it('offers only entities a reviewer may cite', () => {
    expect(citableRefs(row).map((r) => r.ref)).toEqual(['person.x', 'alias.x'])
  })

  it('keeps only positive decimal ids', () => {
    expect(useIDsFrom({ 'person.x': ' 12 ', 'alias.x': '', 'org.y': 'abc', 'org.z': '0' })).toEqual({ 'person.x': '12' })
  })
})
