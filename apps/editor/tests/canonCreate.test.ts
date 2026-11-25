import { describe, expect, it } from 'vitest'
import { CANON_CREATE_TEMPLATES } from '../app/constants/canonCreate'
import {
  changesFor,
  draftsFor,
  judgedRef,
  missingFields,
  resolvedChanges
} from '../app/utils/canon/create'

const prop = (key: string, shape = 'text') => ({
  key,
  shape,
  doc: '',
  clauses: [],
  agent_may: true,
  immutable: false
})
const schemas = [
  { name: 'work', children: {}, properties: [prop('medium', 'vocab'), prop('olang'), prop('status', 'vocab'), prop('content_rating', 'vocab')] },
  { name: 'work_title', children: {}, properties: [prop('work_id', 'ref'), prop('text'), prop('lang'), prop('kind', 'vocab')] },
  { name: 'book_work', children: {}, properties: [prop('category', 'vocab')] },
  { name: 'work_part', children: {}, properties: [prop('kind', 'vocab'), prop('label'), prop('ordinal', 'ordinal'), prop('released')] },
  { name: 'release', children: {}, properties: [prop('lang'), prop('format', 'vocab'), prop('edition_kind', 'vocab'), prop('release_status', 'vocab')] },
  { name: 'book_release', children: {}, properties: [prop('isbn13'), prop('pages', 'integer')] },
  { name: 'release_content', children: {}, properties: [prop('part_id', 'ref'), prop('portion', 'vocab')] }
]
const work = CANON_CREATE_TEMPLATES.work!
const volume = CANON_CREATE_TEMPLATES.volume!

describe('canon creation', () => {
  it('asks for what must be there', () => {
    expect(missingFields(work, draftsFor(work)).map((m) => m.field)).toEqual(['text'])
  })

  it('links new rows to each other and to where the form was opened', () => {
    const drafts = draftsFor(volume)
    drafts.part!.label = '3'
    drafts.part!.ordinal = '3'
    drafts.book!.isbn13 = '9784049164671'
    const changes = changesFor(volume, drafts, schemas, { work: '42' })
    expect(changes.map((c) => c.entity)).toEqual(['work_part', 'release', 'book_release', 'release_content'])
    expect(changes[0]!.values).toMatchObject({ kind: 'volume', label: '3', ordinal: 3, work_id: '42' })
    expect(changes[2]!.values).toEqual({ isbn13: '9784049164671', release_id: 'ref:release' })
    expect(changes[3]!.values).toEqual({ release_id: 'ref:release', work_id: '42', part_id: 'ref:part' })
  })

  it('writes no optional row left empty, and keeps the structural one', () => {
    const changes = changesFor(volume, draftsFor(volume), schemas, { work: '42' })
    expect(changes.map((c) => c.entity)).toEqual(['work_part', 'release', 'release_content'])
  })

  it('takes over one candidate and tells the rest apart', () => {
    const candidates = [
      { token: 'legacy:7', id: '7', label: 'a' },
      { token: 'canon:9', id: '9', label: 'b' }
    ]
    const changes = [{ ref: 'work', entity: 'work', values: {} }, { ref: 'title', entity: 'work_title', values: {} }]
    expect(judgedRef(work)).toBe('work')
    expect(resolvedChanges(changes, 'work', candidates, '7')[0]).toMatchObject({ adopt_id: '7', distinct: ['canon:9'] })
    expect(resolvedChanges(changes, 'work', candidates, '')[0]).toMatchObject({ distinct: ['legacy:7', 'canon:9'] })
    expect(resolvedChanges(changes, 'work', candidates, '7')[1]).toEqual(changes[1])
  })

  it('judges a person by the name, not by the bare person', () => {
    expect(judgedRef(CANON_CREATE_TEMPLATES.person!)).toBe('alias')
  })
})
