import { describe, expect, it } from 'vitest'
import {
  CREATABLE_FAMILIES,
  MERGEABLE_FAMILIES
} from '../app/constants/creation'
import { EDIT_CONFIGS } from '../app/constants/editEntities'
import {
  buildCreationBody,
  duplicateSuspects,
  mergeTiming,
  missingRequired,
  parseRefs,
  parseReleaseDate
} from '../app/utils/creation'
import { conflictFieldKeys, problemCode } from '../app/utils/review/problem'
import { formatWait, reapplyWaitMs } from '../shared/utils/application'
import { resolveEditControl } from '../shared/utils/editControl'

const listField = (vocabulary?: string) =>
  ({
    key: 'catalog.release.platforms',
    field_type: 'list',
    element: { type: 'text' },
    ...(vocabulary ? { vocabulary } : {})
  }) as never

describe('vocabulary lists', () => {
  it('render a text list with a vocabulary as a multi-select', () => {
    expect(resolveEditControl(listField('platform'))).toBe('vocabList')
    expect(resolveEditControl(listField())).toBe('stringList')
  })

  it('let a config name the vocabulary when the schema does not', () => {
    const config = EDIT_CONFIGS.work.fields['catalog.work.platforms']!
    expect(config.vocabulary).toBe('platform')
    expect(resolveEditControl(listField(), config)).toBe('vocabList')
  })
})

describe('work configs for the new fields', () => {
  it('keep series placement to the existing memberships', () => {
    const config = EDIT_CONFIGS.work.fields['catalog.work.series_members']!
    expect(config.fixedRows).toBe(true)
    expect(config.members?.series_id?.readonly).toBe(true)
  })

  it('pick works, companies and releases for the new members', () => {
    const fields = EDIT_CONFIGS.work.fields
    expect(fields['catalog.work.relations']?.members?.work_id?.ref).toBe('work')
    expect(fields['catalog.work.credits']?.members?.release_id?.ref).toBe(
      'release'
    )
    expect(fields['catalog.work.credits']?.members?.label_id?.ref).toBe(
      'company'
    )
    expect(fields['catalog.work.tag_spoilers']?.members?.tag_id?.ref).toBe(
      'tag'
    )
  })

  it('point the trait group at another trait', () => {
    const config = EDIT_CONFIGS.trait.fields['catalog.trait.group_id']!
    expect(config.control).toBe('ref')
    expect(config.ref).toBe('trait')
  })
})

describe('reapply countdown', () => {
  const now = Date.parse('2026-10-20T00:00:00Z')

  it('is zero when there is nothing to wait for', () => {
    expect(reapplyWaitMs(null, now)).toBe(0)
    expect(reapplyWaitMs('not a date', now)).toBe(0)
    expect(reapplyWaitMs('2026-10-19T00:00:00Z', now)).toBe(0)
  })

  it('counts down to reapply_after', () => {
    expect(reapplyWaitMs('2026-10-20T01:30:00Z', now)).toBe(90 * 60_000)
  })

  it('reads in minutes, hours or days', () => {
    expect(formatWait(30_000)).toBe('1 分钟')
    expect(formatWait(90 * 60_000)).toBe('1 小时 30 分钟')
    expect(formatWait(2 * 3_600_000)).toBe('2 小时')
    expect(formatWait(26 * 3_600_000)).toBe('1 天 2 小时')
  })
})

describe('revert conflicts', () => {
  it('reads the code and the conflicting field keys', () => {
    const error = {
      status: 409,
      data: {
        code: 'REVERT_CONFLICT',
        errors: [
          {
            pointer: '/fields/catalog.work.titles',
            reason: 'inconsistent_with'
          },
          { pointer: '/body/mode' }
        ]
      }
    }
    expect(problemCode(error)).toBe('REVERT_CONFLICT')
    expect(conflictFieldKeys(error)).toEqual(['catalog.work.titles'])
    expect(conflictFieldKeys(null)).toEqual([])
  })
})

describe('creations', () => {
  it('reads duplicate suspects only from DUPLICATE_SUSPECTS', () => {
    const suspects = [{ id: '7', display_name: '鲲' }]
    expect(
      duplicateSuspects({ data: { code: 'DUPLICATE_SUSPECTS', suspects } })
    ).toEqual(suspects)
    expect(duplicateSuspects({ data: { code: 'DUPLICATE_SUSPECTS' } })).toEqual(
      []
    )
    expect(
      duplicateSuspects({ data: { code: 'VALIDATION_FAILED' } })
    ).toBeNull()
  })

  it('builds a body without blank values and confirms only when asked', () => {
    expect(
      buildCreationBody({
        object: 'release',
        values: {
          'catalog.release.kind': 'complete',
          'catalog.release.title': '  ',
          'catalog.release.lang': null
        },
        params: { work_id: 4, extra: null },
        note: ' 来源 '
      })
    ).toEqual({
      object: 'release',
      patch: { 'catalog.release.kind': 'complete' },
      params: { work_id: 4 },
      note: '来源'
    })
    expect(
      buildCreationBody({
        object: 'engine',
        values: { 'catalog.engine.name': 'KiriKiri' },
        params: {},
        note: '',
        confirmDuplicates: true
      })
    ).toEqual({
      object: 'engine',
      patch: { 'catalog.engine.name': 'KiriKiri' },
      confirm_duplicates: true
    })
  })

  it('lists the required fields still empty', () => {
    expect(missingRequired(['a', 'b', 'c'], { a: 'x', b: ' ', c: [] })).toEqual(
      ['b', 'c']
    )
  })

  it('parses refs and partial release dates for a new work', () => {
    expect(parseRefs('vndb:v17, Bangumi:123 junk')).toEqual([
      { source: 'vndb', external_id: 'v17' },
      { source: 'bangumi', external_id: '123' }
    ])
    expect(parseReleaseDate('2018')).toEqual({ y: 2018 })
    expect(parseReleaseDate('2018-04')).toEqual({ y: 2018, m: 4 })
    expect(parseReleaseDate('2018-04-25')).toEqual({ y: 2018, m: 4, d: 25 })
    expect(parseReleaseDate('2018-13')).toBeNull()
    expect(parseReleaseDate('soon')).toBeNull()
  })
})

describe('merge request timing', () => {
  const now = Date.parse('2026-10-20T00:00:00Z')

  it('lets approved requests execute only after the cooling-off', () => {
    expect(mergeTiming('open', null, now)).toEqual({
      canExecute: false,
      waitMs: 0
    })
    expect(mergeTiming('approved', '2026-10-20T02:00:00Z', now)).toEqual({
      canExecute: false,
      waitMs: 2 * 3_600_000
    })
    expect(mergeTiming('approved', '2026-10-19T00:00:00Z', now)).toEqual({
      canExecute: true,
      waitMs: 0
    })
  })

  it('offers merging only for families the merge machinery supports', () => {
    expect(MERGEABLE_FAMILIES).toEqual([
      'work',
      'company',
      'character',
      'person',
      'credit_name'
    ])
    expect(CREATABLE_FAMILIES).not.toContain('work')
  })
})
