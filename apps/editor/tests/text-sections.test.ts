import { describe, expect, it } from 'vitest'
import type { EditUpstreamRow } from '../shared/types/edit'
import {
  mergeRows,
  restoreRemovedRow,
  summarizeMerged,
  toggleSuppression
} from '../app/utils/mergedRows'
import {
  classifyWorkLink,
  contextLinkUrl,
  linkFingerprint,
  siteOfUrl
} from '../app/utils/externalLinks'
import { introLanguages, titleLanguages } from '../app/utils/languageCoverage'

const upstreamRow = (
  key: string,
  extra: Partial<EditUpstreamRow> = {}
): EditUpstreamRow => ({
  key,
  identity: key,
  label: key,
  ...extra
})

const identityOf = (row: unknown) => String(row)

describe('mergeRows', () => {
  it('marks human rows as added, changed or kept against the base', () => {
    const rows = mergeRows({
      draft: [
        { id: 1, v: 'a' },
        { id: 2, v: 'B' },
        { id: 3, v: 'c' }
      ],
      base: [
        { id: 1, v: 'a' },
        { id: 2, v: 'b' }
      ],
      upstream: [],
      suppressed: [],
      baseSuppressed: [],
      identityOf: (row) => String((row as { id: number }).id)
    })
    expect(rows.map((row) => [row.lane, row.status])).toEqual([
      ['human', 'kept'],
      ['human', 'changed'],
      ['human', 'added']
    ])
  })

  it('keeps removed base rows in the list so they can be restored', () => {
    const rows = mergeRows({
      draft: ['b'],
      base: ['a', 'b'],
      upstream: [],
      suppressed: [],
      baseSuppressed: [],
      identityOf
    })
    const removed = rows.find((row) => row.status === 'removed')!
    expect(removed.value).toBe('a')
    expect(rows.map((row) => row.value)).toEqual(['a', 'b'])
    expect(restoreRemovedRow(['b'], ['a', 'b'], removed)).toEqual(['a', 'b'])
  })

  it('folds upstream rows that a human row duplicates', () => {
    const rows = mergeRows({
      draft: ['x'],
      base: [],
      upstream: [upstreamRow('tag:1', { match: 'x' }), upstreamRow('tag:2')],
      suppressed: [],
      baseSuppressed: [],
      identityOf
    })
    expect(rows.map((row) => row.key)).toEqual(['h:0', 'u:tag:2'])
  })

  it('pairs an edited row with the base row in its position instead of add + remove', () => {
    const rows = mergeRows({
      draft: ['a', 'B', 'c'],
      base: ['a', 'b'],
      upstream: [],
      suppressed: [],
      baseSuppressed: [],
      identityOf
    })
    expect(rows.map((row) => [row.value, row.status, row.base])).toEqual([
      ['a', 'kept', 'a'],
      ['B', 'changed', 'b'],
      ['c', 'added', undefined]
    ])
  })

  it('tracks hiding, hidden and unhiding upstream rows and puts hidden ones last', () => {
    const rows = mergeRows({
      draft: [],
      base: [],
      upstream: [
        upstreamRow('a'),
        upstreamRow('b'),
        upstreamRow('c'),
        upstreamRow('d')
      ],
      suppressed: ['a', 'b'],
      baseSuppressed: ['b', 'c'],
      identityOf
    })
    expect(rows.map((row) => [row.identity, row.status])).toEqual([
      ['c', 'unhiding'],
      ['d', 'kept'],
      ['a', 'hiding'],
      ['b', 'hidden']
    ])
  })

  it('lists suppressions whose row is gone as ghost rows', () => {
    const rows = mergeRows({
      draft: [],
      base: [],
      upstream: [upstreamRow('live')],
      suppressed: ['gone'],
      baseSuppressed: ['gone', 'restored'],
      identityOf
    })
    expect(
      rows
        .filter((row) => row.lane === 'ghost')
        .map((row) => [row.identity, row.status])
    ).toEqual([
      ['gone', 'hidden'],
      ['restored', 'unhiding']
    ])
  })

  it('summarizes visible, hidden and changed rows', () => {
    const rows = mergeRows({
      draft: ['new'],
      base: [],
      upstream: [upstreamRow('a'), upstreamRow('b')],
      suppressed: ['b'],
      baseSuppressed: [],
      identityOf
    })
    expect(summarizeMerged(rows)).toEqual({
      visible: 2,
      hidden: 1,
      human: 1,
      upstream: 1,
      changed: 2
    })
  })

  it('toggles a suppression key', () => {
    expect(toggleSuppression(['a'], 'b')).toEqual(['a', 'b'])
    expect(toggleSuppression(['a', 'b'], 'a')).toEqual(['b'])
  })
})

describe('external links', () => {
  it('detects the site of a URL', () => {
    expect(siteOfUrl('https://vndb.org/v20424')?.key).toBe('vndb')
    expect(siteOfUrl('https://ci-en.dlsite.com/creator/123')?.key).toBe('cien')
    expect(
      siteOfUrl('https://www.dlsite.com/maniax/work/=/product_id/RJ01.html')
        ?.key
    ).toBe('dlsite')
    expect(siteOfUrl('https://key.visualarts.gr.jp/summer')).toEqual({
      key: 'web',
      label: 'key.visualarts.gr.jp',
      host: 'key.visualarts.gr.jp'
    })
    expect(siteOfUrl('not a url')).toBeNull()
  })

  it('mints the same URLs as the catalog for stored link rows', () => {
    expect(contextLinkUrl('work', 'vndb', 'v20424')).toBe(
      'https://vndb.org/v20424'
    )
    expect(contextLinkUrl('work', 'erogamescape', '27562')).toBe(
      'https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/game.php?game=27562'
    )
    expect(
      contextLinkUrl('work', 'official_site', 'key.visualarts.gr.jp/summer')
    ).toBe('https://key.visualarts.gr.jp/summer')
    expect(contextLinkUrl('company', 'vndb', 'p24')).toBe(
      'https://vndb.org/p24'
    )
    expect(contextLinkUrl('work', 'vndb', 'p24')).toBeNull()
  })

  it('classifies typed URLs the way the catalog does', () => {
    expect(classifyWorkLink('https://vndb.org/V123/')).toEqual({
      source: 'vndb',
      externalId: 'v123',
      kind: 'probable'
    })
    expect(classifyWorkLink('https://x.com/key_official')).toEqual({
      source: 'twitter',
      externalId: 'key_official',
      kind: 'related'
    })
    expect(classifyWorkLink('https://twitter.com/home')?.source).toBe('web')
    expect(classifyWorkLink('ftp://example.com')).toBeNull()
  })

  it('fingerprints web links without scheme or trailing slash', () => {
    expect(linkFingerprint('web', 'https://Example.com/a/')).toBe(
      linkFingerprint('official_site', 'example.com/a').replace(
        'official_site',
        'web'
      )
    )
  })
})

describe('language coverage', () => {
  it('orders intro languages original first, then the common ones, and marks status', () => {
    const langs = introLanguages({
      olang: 'ja',
      human: [{ lang: 'en' }],
      upstream: [
        { lang: 'zh-Hans', hidden: false, source: 'VNDB', machine: true },
        { lang: 'ko', hidden: true, source: 'VNDB' }
      ]
    })
    expect(langs.map((entry) => [entry.lang, entry.status])).toEqual([
      ['ja', 'missing'],
      ['zh-Hans', 'upstream'],
      ['zh-Hant', 'missing'],
      ['en', 'human'],
      ['ko', 'missing']
    ])
    expect(langs[1]).toMatchObject({ machine: true, displaySource: 'VNDB' })
    expect(langs[4]).toMatchObject({ hiddenCount: 1, upstreamCount: 0 })
  })

  it('counts titles per language and lists the common missing ones', () => {
    const langs = titleLanguages({
      olang: 'ja',
      titles: [
        { lang: 'ja', kind: 0 },
        { lang: 'ja', kind: 1 },
        { lang: '', kind: 1 }
      ]
    })
    expect(
      langs.map((entry) => [entry.lang, entry.count, entry.missing])
    ).toEqual([
      ['ja', 2, false],
      ['zh-Hans', 0, true],
      ['zh-Hant', 0, true],
      ['en', 0, true],
      ['', 1, false]
    ])
    expect(langs[0]).toMatchObject({ official: true, isOriginal: true })
  })
})
