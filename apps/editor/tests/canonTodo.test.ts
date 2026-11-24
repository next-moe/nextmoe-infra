import { describe, expect, it } from 'vitest'
import {
  answerOf,
  answerOptions,
  answersFrom,
  showValue,
  todoAsks,
  todoTitle,
  unanswered
} from '../app/utils/canon/todo'

const author = {
  ref: 'alias.x',
  entity: 'person_alias',
  name: '川原 礫',
  why: 'a new person_alias of this name',
  cites: false,
  candidates: [
    { token: 'canon:12', id: 12, label: '川原礫', facts: [] },
    { token: 'legacy:7', id: 7, label: '川原 礫', facts: [] }
  ]
}

describe('canon todo sheet', () => {
  it('shows a cited id by its name', () => {
    expect(showValue({ value: 'ref:work' })).toBe('→ work')
    expect(showValue({ value: 5, is: '電撃文庫' })).toBe('電撃文庫（5）')
    expect(showValue({ value: 28 })).toBe('28')
  })

  it('offers every candidate and a new entity', () => {
    expect(answerOptions(author).map((o) => o.value)).toEqual([
      'canon:12',
      'legacy:7',
      'new'
    ])
  })

  it('offers no new entity where the row cites a known one', () => {
    expect(answerOptions({ ...author, cites: true }).map((o) => o.value)).toEqual([
      'canon:12',
      'legacy:7'
    ])
  })

  it('uses a canon id and takes over a legacy one', () => {
    expect(answerOf('canon:12')).toEqual({ use: '12' })
    expect(answerOf('legacy:7')).toEqual({ adopt: '7' })
    expect(answerOf('new')).toEqual({ new: true })
  })

  it('asks nothing about what is left out', () => {
    expect(unanswered([author], {}, [])).toHaveLength(1)
    expect(unanswered([author], {}, ['alias.x'])).toHaveLength(0)
    expect(answersFrom([author], { 'alias.x': 'new' }, ['alias.x'])).toEqual({})
    expect(answersFrom([author], { 'alias.x': 'legacy:7' }, [])).toEqual({
      'alias.x': { adopt: '7' }
    })
  })
})

describe('canon todo line', () => {
  const todo = {
    id: '1',
    kind: 'first_write' as const,
    subject_key: 'lead:p:l:abc',
    program: 'ln-pilot',
    state: 'open' as const,
    detail: '',
    source: '',
    resolved_by: '',
    created_at: '',
    questions: [{ ref: 'alias.a', entity: 'person_alias', name: '鏡遊' }],
    suggestion: {
      changeset: {
        entities: [
          { ref: 'work', entity: 'work', properties: { medium: { value: 'novel' } } },
          { ref: 't', entity: 'work_title', properties: { work_id: { value: 'ref:work' }, text: { value: '庭' } } },
          { ref: 'b', entity: 'book_release', properties: { isbn13: { value: '9784049164671' } } }
        ]
      }
    }
  }

  it('names the title and the ISBN, and what is asked', () => {
    expect(todoTitle(todo)).toBe('庭 · 9784049164671')
    expect(todoAsks(todo)).toBe('「鏡遊」是哪一个')
  })

  it('names the field of a single value, and falls back to the subject', () => {
    expect(
      todoTitle({
        ...todo,
        suggestion: {
          candidate: { target: { entity: 'release', id: 9 }, property: 'released', value: '2025-08-08' }
        }
      })
    ).toBe('release #9 · released = 2025-08-08')
    expect(todoTitle({ ...todo, suggestion: {} })).toBe('lead:p:l:abc')
  })
})
