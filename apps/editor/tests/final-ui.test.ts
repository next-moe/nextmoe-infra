import { describe, expect, it } from 'vitest'
import { EDIT_CONFIGS } from '../app/constants/editEntities'
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
