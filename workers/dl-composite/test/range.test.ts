import { describe, expect, it } from 'vitest'
import { ifRangeHolds, noneMatch, parseRange, UNSATISFIABLE } from '../src/range'

describe('parseRange', () => {
  const cases: Array<[string | null, ReturnType<typeof parseRange>]> = [
    [null, null],
    ['bytes=0-99', { start: 0, end: 100 }],
    ['bytes=100-', { start: 100, end: 1000 }],
    ['bytes=-100', { start: 900, end: 1000 }],
    ['bytes=-5000', { start: 0, end: 1000 }],
    ['bytes=990-5000', { start: 990, end: 1000 }],
    ['bytes=1000-', UNSATISFIABLE],
    ['bytes=-0', UNSATISFIABLE],
    ['bytes=5-1', null],
    ['bytes=0-1,5-6', null],
    ['items=0-1', null],
    ['bytes=-', null],
  ]
  for (const [header, want] of cases) {
    it(`${header} of 1000`, () => expect(parseRange(header, 1000)).toEqual(want))
  }
})

describe('validators', () => {
  const etag = '"abc"'
  const lm = 'Wed, 14 Oct 2026 12:00:00 GMT'
  it('If-Range holds only on the strong etag or the exact date', () => {
    expect(ifRangeHolds(null, etag, lm)).toBe(true)
    expect(ifRangeHolds('"abc"', etag, lm)).toBe(true)
    expect(ifRangeHolds('W/"abc"', etag, lm)).toBe(false)
    expect(ifRangeHolds('"old"', etag, lm)).toBe(false)
    expect(ifRangeHolds(lm, etag, lm)).toBe(true)
    expect(ifRangeHolds('Wed, 14 Oct 2026 12:00:01 GMT', etag, lm)).toBe(false)
  })
  it('If-None-Match compares weakly', () => {
    expect(noneMatch(null, etag)).toBe(false)
    expect(noneMatch('"x", W/"abc"', etag)).toBe(true)
    expect(noneMatch('*', etag)).toBe(true)
    expect(noneMatch('"x"', etag)).toBe(false)
  })
})
