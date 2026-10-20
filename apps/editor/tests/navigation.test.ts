import { describe, expect, it } from 'vitest'
import { EDITOR_NAV, currentNavPath } from '../app/constants/navigation'

const items = EDITOR_NAV.flatMap((group) => group.items)

describe('current sidebar item', () => {
  it('marks the dashboard only on the root path', () => {
    expect(currentNavPath('/', items)).toBe('/')
    expect(currentNavPath('/works', items)).toBe('/works')
  })

  it('marks the list for its detail pages', () => {
    expect(currentNavPath('/works/42', items)).toBe('/works')
    expect(currentNavPath('/me/proposals/7', items)).toBe('/me/proposals')
  })

  it('prefers the most specific item when two prefixes match', () => {
    expect(currentNavPath('/review/merges', items)).toBe('/review/merges')
    expect(currentNavPath('/review/123', items)).toBe('/review')
  })

  it('does not confuse a shared word prefix with a path segment', () => {
    expect(currentNavPath('/worksheet', items)).toBeNull()
  })

  it('returns null outside the navigation', () => {
    expect(currentNavPath('/apply', items)).toBeNull()
  })
})
