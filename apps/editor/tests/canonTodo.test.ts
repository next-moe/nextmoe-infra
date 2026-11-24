import { describe, expect, it } from 'vitest'
import {
  answerOf,
  answerOptions,
  answersFrom,
  showValue,
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
