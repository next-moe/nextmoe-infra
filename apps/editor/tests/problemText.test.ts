import { describe, expect, it } from 'vitest'
import { explainFieldError } from '../shared/utils/fieldErrorText'
import { memberLabel } from '../app/utils/format'
import { problemMessage } from '../app/utils/review/problem'

const TITLES = 'catalog.work.titles'

const claimFailure = (detail: string) => ({
  statusCode: 422,
  data: {
    status: 422,
    code: 'VALIDATION_FAILED',
    title: 'Validation failed',
    detail: `${TITLES}: ${detail}`,
    errors: [
      {
        pointer: `/field_values/${TITLES}`,
        reason: 'INVALID_FORMAT',
        detail: `${TITLES}: ${detail}`
      }
    ]
  }
})

describe('plain-language validation messages', () => {
  it('names the field, the row and the member once', () => {
    const message = problemMessage(
      claimFailure('element 0: latin must be at most 500 characters'),
      '创建失败'
    )
    expect(message).toBe('「标题」第 1 条：罗马字不能超过 500 个字符')
  })

  it('explains the rules a user can fix', () => {
    const explain = (detail: string) =>
      explainFieldError(TITLES, detail, memberLabel)
    expect(explain('element 1: title must not be empty')).toBe(
      '第 2 条：标题不能为空'
    )
    expect(
      explain('element 0: only an alias (kind 1) may have an empty lang')
    ).toContain('请为这个标题选择语言')
    expect(explain('must contain at least one official (kind 0) title')).toBe(
      '至少需要一个「正式名」类型的标题'
    )
    expect(explain('element 2: duplicate (lang, title, kind)')).toBe(
      '第 3 条：和其他条目重复了'
    )
    expect(
      explainFieldError(
        'catalog.work.links',
        'editing: field "catalog.work.links": element 0: must be an http:// or https:// URL'
      )
    ).toBe('第 1 条：链接需要以 http:// 或 https:// 开头')
  })

  it('does not repeat a scalar field name as a member', () => {
    expect(
      explainFieldError(
        'catalog.character.height_cm',
        'height_cm must be between 1 and 1000'
      )
    ).toBe('需要在 1 到 1000 之间')
  })

  it('owns a shape error as an editor fault instead of blaming the user', () => {
    const message = explainFieldError(
      TITLES,
      'element 0: latin must be a string'
    )
    expect(message).toContain('可能是编辑器的问题')
    expect(message).toContain('element 0: latin must be a string')
  })

  it('does not repeat the same detail twice', () => {
    const message = problemMessage({
      statusCode: 409,
      data: {
        status: 409,
        code: 'SOMETHING_NEW',
        detail: 'same text',
        errors: [
          { pointer: '/x', reason: 'UNKNOWN_VALUE', detail: 'same text' }
        ]
      }
    })
    expect(message).toBe('same text')
  })

  it('keeps the caller fallback when there is no problem body', () => {
    expect(problemMessage(new Error('boom'), '创建失败')).toBe('创建失败')
  })
})
