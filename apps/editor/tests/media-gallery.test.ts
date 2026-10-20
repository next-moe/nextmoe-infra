import { describe, expect, it } from 'vitest'
import {
  applyOrder,
  buildMediaTiles,
  coverKindLabel,
  mediaRowChanges,
  contextMediaRows,
  displaySequence,
  moveHash,
  orderResetPending,
  orderValueFor,
  removedHumanRows,
  setPinned,
  setRowMember,
  type MediaState
} from '../app/utils/mediaGallery'

const state = (patch: Partial<MediaState> = {}): MediaState => ({
  human: [{ image_hash: 'h1' }],
  baseHuman: [{ image_hash: 'h1' }],
  upstream: [
    {
      image_hash: 'u1',
      kind: 'main',
      portrait_pinned: true,
      sexual: 0,
      violence: 0
    },
    {
      image_hash: 'u2',
      kind: '',
      portrait_pinned: false,
      sexual: 1,
      violence: 0
    }
  ],
  baseUpstream: [
    {
      image_hash: 'u1',
      kind: 'main',
      portrait_pinned: true,
      sexual: 0,
      violence: 0
    },
    {
      image_hash: 'u2',
      kind: '',
      portrait_pinned: false,
      sexual: 1,
      violence: 0
    }
  ],
  suppressed: [],
  baseSuppressed: [],
  order: [],
  baseOrder: [],
  context: [
    { hash: 'u1', human: false, identity: 'cover:u1', source: 'VNDB' },
    { hash: 'h1', human: true, identity: null, source: '人工' },
    { hash: 'u2', human: false, identity: 'cover:u2', source: 'Getchu' }
  ],
  ...patch
})

describe('media gallery model', () => {
  it('reads present context rows only', () => {
    const rows = contextMediaRows({
      key: 'catalog.work.covers',
      rows: [
        {
          present: true,
          human_lane: false,
          identity: 'cover:a',
          value: { image_hash: 'a' },
          source: { key: 'vndb', display: 'VNDB' }
        },
        {
          present: false,
          human_lane: false,
          identity: 'cover:gone',
          value: null,
          source: null
        }
      ]
    } as never)
    expect(rows).toEqual([
      { hash: 'a', human: false, identity: 'cover:a', source: 'VNDB' }
    ])
  })

  it('shows the context order and appends new uploads', () => {
    const s = state({ human: [{ image_hash: 'h1' }, { image_hash: 'new' }] })
    expect(displaySequence(s)).toEqual(['u1', 'h1', 'u2', 'new'])
    const tiles = buildMediaTiles(s)
    expect(tiles.map((t) => [t.hash, t.lane, t.isNew])).toEqual([
      ['u1', 'upstream', false],
      ['h1', 'human', false],
      ['u2', 'upstream', false],
      ['new', 'human', true]
    ])
    expect(tiles[0]!.source).toBe('VNDB')
    expect(tiles[0]!.identity).toBe('cover:u1')
  })

  it('applies a draft order and keeps unlisted images after it', () => {
    expect(applyOrder(['a', 'b', 'c'], ['c', 'x', 'a'])).toEqual([
      'c',
      'a',
      'b'
    ])
    const s = state({ order: ['u2', 'h1'] })
    expect(displaySequence(s)).toEqual(['u2', 'h1', 'u1'])
  })

  it('marks hidden and regraded upstream tiles as changed', () => {
    const s = state({
      suppressed: ['cover:u2'],
      upstream: [
        {
          image_hash: 'u1',
          kind: 'main',
          portrait_pinned: true,
          sexual: 2,
          violence: 0
        },
        {
          image_hash: 'u2',
          kind: '',
          portrait_pinned: false,
          sexual: 1,
          violence: 0
        }
      ]
    })
    const tiles = buildMediaTiles(s)
    expect(tiles.find((t) => t.hash === 'u1')!.changed).toBe(true)
    const u2 = tiles.find((t) => t.hash === 'u2')!
    expect(u2.hidden).toBe(true)
    expect(u2.changed).toBe(true)
    expect(tiles.find((t) => t.hash === 'h1')!.changed).toBe(false)
  })

  it('moves a hash and drops the order when it comes back to natural', () => {
    const s = state()
    const moved = moveHash(['u1', 'h1', 'u2'], 'u2', 0)
    expect(moved).toEqual(['u2', 'u1', 'h1'])
    expect(moveHash(['u1', 'h1', 'u2'], 'u1', 3)).toEqual(['h1', 'u2', 'u1'])
    expect(orderValueFor(moved, s)).toEqual(['u2', 'u1', 'h1'])
    expect(orderValueFor(['u1', 'h1', 'u2'], s)).toEqual([])
    expect(
      orderValueFor(
        ['u1', 'h1', 'u2'],
        state({ order: ['x'], baseOrder: ['u1', 'h1', 'u2'] })
      )
    ).toEqual(['u1', 'h1', 'u2'])
  })

  it('flags a reset of a stored order', () => {
    expect(orderResetPending(state({ baseOrder: ['u1'] }))).toBe(true)
    expect(orderResetPending(state())).toBe(false)
  })

  it('pins exactly one image across both lanes', () => {
    const s = state()
    const next = setPinned(s, 'h1')
    expect(next.human).toEqual([{ image_hash: 'h1', portrait_pinned: true }])
    expect(next.upstream.map((r) => r.portrait_pinned)).toEqual([false, false])
    const cleared = setPinned(next, null)
    expect(cleared.human).toEqual([{ image_hash: 'h1' }])
    expect(cleared.upstream[1]).toBe(next.upstream[1])
  })

  it('omits empty members on human rows only when asked', () => {
    const rows = [{ image_hash: 'h1', sexual: 1 }]
    expect(setRowMember(rows, 0, 'sexual', 0, true)).toEqual([
      { image_hash: 'h1' }
    ])
    expect(setRowMember(rows, 0, 'sexual', 0, false)).toEqual([
      { image_hash: 'h1', sexual: 0 }
    ])
  })

  it('lists human rows the draft removed', () => {
    expect(
      removedHumanRows(state({ human: [] })).map((r) => r.image_hash)
    ).toEqual(['h1'])
  })

  it('labels cover kinds and keeps unknown ones verbatim', () => {
    expect(coverKindLabel('pkgfront')).toBe('包装正面')
    expect(coverKindLabel('')).toBe('未分类')
    expect(coverKindLabel('poster')).toBe('poster')
  })

  it('describes a regrade in plain language', () => {
    expect(
      mediaRowChanges(
        {
          image_hash: 'u1',
          kind: 'dig',
          portrait_pinned: false,
          sexual: 0,
          violence: 0
        },
        {
          image_hash: 'u1',
          kind: 'main',
          portrait_pinned: true,
          sexual: 2,
          violence: 0
        }
      )
    ).toEqual([
      { label: '主封面', before: '否', after: '是' },
      { label: '类型', before: '数字版封面', after: '主视觉' },
      { label: '性暗示', before: '安全', after: '露骨' }
    ])
  })
})
