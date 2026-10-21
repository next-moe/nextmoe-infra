import { describe, expect, it } from 'vitest'
import type {
  CatalogSplitPreviewRelease,
  CatalogSplitRequest
} from '../shared/types/catalog'
import {
  draftBody,
  draftProblems,
  groupReleases,
  parseRefKey,
  previewQuery,
  rangeSelect,
  refKey,
  releaseMatches,
  sameDraft
} from '../app/utils/split/plan'
import { splitTimeline } from '../app/utils/split/timeline'
import { mergeTimeline } from '../app/utils/merge/timeline'

const release = (
  id: string,
  over: Partial<CatalogSplitPreviewRelease> = {}
): CatalogSplitPreviewRelease => ({
  id,
  title: `Rewrite ${id}`,
  release_kind: 'default',
  platform: 'win',
  lang: 'ja',
  release_date: '2016-07-29',
  release_date_precision: 'day',
  vndb_rid: null,
  sources: [],
  label_count: 0,
  credit_count: 0,
  selected: false,
  tombstone: false,
  ...over
})

describe('rangeSelect', () => {
  const visible = ['1', '2', '3', '4', '5']
  it('toggles one release without shift', () => {
    expect(rangeSelect(visible, [], null, '2', false)).toEqual(['2'])
    expect(rangeSelect(visible, ['2'], '2', '2', false)).toEqual([])
  })
  it('selects the span to the anchor state with shift', () => {
    expect(rangeSelect(visible, ['2'], '2', '4', true).sort()).toEqual([
      '2',
      '3',
      '4'
    ])
  })
  it('clears the span when the anchor is unselected', () => {
    expect(rangeSelect(visible, ['1', '3', '4', '5'], '2', '4', true)).toEqual([
      '1',
      '5'
    ])
  })
  it('falls back to a toggle when the anchor is filtered out', () => {
    expect(rangeSelect(['3', '4'], ['9'], '9', '4', true)).toEqual(['9', '4'])
  })
})

describe('grouping and filtering', () => {
  const list = [
    release('1', { platform: 'win', lang: 'ja' }),
    release('2', { platform: 'ps4', lang: 'en', vndb_rid: 'r49193' }),
    release('3', { platform: 'win', lang: 'en', release_kind: 'trial' })
  ]
  it('groups by platform in first-seen order', () => {
    expect(
      groupReleases(list, 'platform').map((g) => [
        g.key,
        g.releases.map((r) => r.id)
      ])
    ).toEqual([
      ['win', ['1', '3']],
      ['ps4', ['2']]
    ])
  })
  it('matches the VNDB release id', () => {
    expect(
      list.filter((r) => releaseMatches(r, 'R49193')).map((r) => r.id)
    ).toEqual(['2'])
  })
})

describe('request bodies', () => {
  const selection = { releases: ['10', '9'], promote: ['bangumi:491620'] }
  const draft = {
    displayName: ' Rewrite+ ',
    olang: 'ja',
    titles: [
      { lang: 'ja', title: ' Rewrite+ ', title_kind: 'official' as const },
      { lang: '', title: '  ', title_kind: 'alias' as const }
    ]
  }
  it('sorts releases, trims the draft and drops empty titles', () => {
    expect(draftBody(selection, draft)).toEqual({
      release_ids: ['9', '10'],
      display_name: 'Rewrite+',
      olang: 'ja',
      titles: [{ lang: 'ja', title: 'Rewrite+', title_kind: 'official' }],
      promote_refs: [{ source: 'bangumi', external_id: '491620' }]
    })
  })
  it('treats order and whitespace as no change', () => {
    expect(
      sameDraft(
        { selection, draft },
        {
          selection: { releases: ['9', '10'], promote: ['bangumi:491620'] },
          draft: { ...draft, displayName: 'Rewrite+' }
        }
      )
    ).toBe(true)
  })
  it('lists what is still missing', () => {
    expect(
      draftProblems(
        { releases: [], promote: [] },
        { displayName: '', olang: 'Japanese', titles: [] }
      )
    ).toHaveLength(3)
    expect(draftProblems(selection, draft)).toEqual([])
  })
  it('builds the preview query', () => {
    expect(previewQuery('198', selection)).toEqual({
      object: 'work',
      target_id: '198',
      release_ids: '9,10',
      promote: 'bangumi:491620'
    })
    expect(previewQuery('198', { releases: [], promote: [] })).toEqual({
      object: 'work',
      target_id: '198'
    })
  })
  it('round-trips ref keys with colons in the id', () => {
    expect(
      parseRefKey(refKey({ source: 'erogamescape', external_id: 'a:b' }))
    ).toEqual({
      source: 'erogamescape',
      external_id: 'a:b'
    })
  })
})

describe('timelines', () => {
  const base = {
    proposed_at: '2026-10-21T00:00:00Z',
    execute_after: '2026-10-21T01:00:00Z',
    executed_at: '2026-10-21T02:00:00Z',
    undone_at: '2026-10-21T03:00:00Z',
    undo_until: null
  }
  it('uses split wording for a split', () => {
    const steps = splitTimeline(
      { ...base, state: 'undone' } as unknown as CatalogSplitRequest,
      Date.parse('2026-10-22T00:00:00Z')
    )
    expect(steps.map((s) => s.label)).toEqual([
      '提交',
      '审核',
      '冷静期',
      '执行拆分',
      '撤销拆分'
    ])
  })
  it('keeps merge wording for a merge', () => {
    const steps = mergeTimeline(
      { ...base, state: 'executed' } as never,
      Date.parse('2026-10-22T00:00:00Z')
    )
    expect(steps.find((s) => s.key === 'executed')?.label).toBe('执行合并')
  })
})
