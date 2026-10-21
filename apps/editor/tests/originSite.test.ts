import { describe, expect, it } from 'vitest'
import { isOriginSite, originFromLanding } from '../server/utils/origin-site'

describe('origin site remembered from a deep link', () => {
  it.each([
    ['kungal', 'https://www.kungal.com/galgame/4'],
    ['moyu', 'https://www.moyu.moe/patch/4'],
    ['letmoe', 'https://staging.letmoe.com/games/4'],
    ['nextmoe', 'https://www.nextmoe.com/']
  ])('keeps %s when return points back at it', (from, target) => {
    expect(originFromLanding(from, target)).toBe(from)
  })

  it.each([
    ['kungal', 'https://www.moyu.moe/patch/4'],
    ['kungal', 'http://www.kungal.com/galgame/4'],
    ['editor', 'https://editor.nextmoe.com/'],
    ['galgame_wiki', 'https://www.kungal.com/'],
    ['kungal', undefined],
    [['kungal'], 'https://www.kungal.com/']
  ])('refuses from=%s with return=%s', (from, target) => {
    expect(originFromLanding(from, target)).toBeNull()
  })

  it('accepts only the four linking sites as a stored value', () => {
    expect(isOriginSite('letmoe')).toBe(true)
    expect(isOriginSite('editor')).toBe(false)
    expect(isOriginSite('')).toBe(false)
    expect(isOriginSite(undefined)).toBe(false)
  })
})
