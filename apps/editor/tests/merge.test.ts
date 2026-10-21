import { describe, expect, it } from 'vitest'
import type {
  CatalogMergePreview,
  CatalogMergeRequest
} from '../shared/types/catalog'
import {
  choicesBody,
  countAfter,
  fixedSide,
  initialChoices,
  mergedReleases,
  mergedTitles,
  releaseDateText,
  sameChoices
} from '../app/utils/merge/plan'
import { mergeTimeline } from '../app/utils/merge/timeline'

const preview = (): CatalogMergePreview =>
  ({
    object: 'merge_preview',
    target_object: 'work',
    source: {
      id: '211503',
      name: '脅迫～終わらない明日～',
      live: true,
      site: null
    },
    target: { id: '11529', name: '脅迫', live: true, site: 'kungal' },
    blockers: [],
    warnings: [],
    facts: [],
    fields: [
      {
        key: 'display_name',
        source_value: '脅迫～終わらない明日～',
        target_value: '脅迫',
        default: 'target',
        human_protected: false
      },
      {
        key: 'olang',
        source_value: 'ja',
        target_value: null,
        default: 'source',
        human_protected: false
      }
    ],
    anchors: [],
    releases: [
      {
        side: 'source',
        id: '149250',
        title: '',
        release_kind: 'digital',
        platform: 'win',
        lang: '',
        release_date: '2005-07-15',
        release_date_precision: 'day',
        vndb_rid: null,
        duplicate_of: '180443',
        duplicate_by: 'platform_date'
      },
      {
        side: 'target',
        id: '180443',
        title: 'ダウンロード版',
        release_kind: 'default',
        platform: 'win',
        lang: 'ja',
        release_date: '2005-07-15',
        release_date_precision: 'day',
        vndb_rid: 'r74937',
        duplicate_of: null,
        duplicate_by: null
      },
      {
        side: 'target',
        id: '258726',
        title: '脅迫',
        release_kind: 'default',
        platform: 'p98',
        lang: 'ja',
        release_date: '1996-04-01',
        release_date_precision: 'month',
        vndb_rid: 'r8293',
        duplicate_of: null,
        duplicate_by: null
      }
    ],
    titles: [
      {
        side: 'target',
        title: '脅迫',
        lang: 'ja',
        title_kind: 'official',
        fate: 'stays'
      },
      {
        side: 'source',
        title: '脅迫～終わらない明日～',
        lang: 'ja',
        title_kind: 'official',
        fate: 'becomes_alias'
      },
      {
        side: 'source',
        title: '胁迫～没有终结的明日～',
        lang: 'zh-Hans',
        title_kind: 'official',
        fate: 'moves'
      },
      {
        side: 'source',
        title: '脅迫',
        lang: 'ja',
        title_kind: 'alias',
        fate: 'duplicate'
      }
    ],
    intros: [],
    counts: [],
    claim: null,
    open_request_id: null
  }) as unknown as CatalogMergePreview

describe('merge choices', () => {
  it('pins a field when only one side can survive', () => {
    const [name, olang] = preview().fields
    expect(fixedSide(name!)).toBeNull()
    expect(fixedSide(olang!)).toBe('source')
    expect(fixedSide({ ...name!, human_protected: true })).toBe('target')
  })

  it('preselects hiding the duplicates and keeps the defaults', () => {
    const choices = initialChoices(preview())
    expect(choices.hide).toEqual(['149250'])
    expect(choices.fields).toEqual({ display_name: 'target', olang: 'source' })
  })

  it('reads stored choices back instead of the defaults', () => {
    const choices = initialChoices(preview(), {
      field_resolution: { display_name: 'source' },
      hide_release_ids: []
    })
    expect(choices.fields.display_name).toBe('source')
    expect(choices.hide).toEqual([])
  })

  it('sends only choices that change the outcome', () => {
    const p = preview()
    const body = choicesBody(p, {
      fields: { display_name: 'source', olang: 'source' },
      hide: ['149250']
    })
    expect(body).toEqual({
      field_resolution: { display_name: 'source' },
      hide_release_ids: ['149250']
    })
    expect(sameChoices(p, initialChoices(p), initialChoices(p))).toBe(true)
  })
})

describe('merge result', () => {
  it('turns titles in a taken language into language-less aliases', () => {
    const titles = mergedTitles(preview().titles)
    expect(titles.map((t) => [t.title, t.lang, t.title_kind])).toEqual([
      ['脅迫', 'ja', 'official'],
      ['脅迫～終わらない明日～', '', 'alias'],
      ['胁迫～没有终结的明日～', 'zh-Hans', 'official']
    ])
  })

  it('marks hidden source releases and sorts by date', () => {
    const rows = mergedReleases(preview().releases, ['149250'])
    expect(rows.map((r) => [r.id, r.hidden])).toEqual([
      ['258726', false],
      ['149250', true],
      ['180443', false]
    ])
    expect(releaseDateText(rows[0]!)).toBe('1996-04')
  })

  it('counts folded rows once', () => {
    expect(
      countAfter({
        key: 'tags',
        on_source: 5,
        on_target: 7,
        already_on_target: 3
      })
    ).toBe(9)
  })
})

describe('mergeTimeline', () => {
  const base = {
    id: '1',
    state: 'open',
    proposed_at: '2026-10-20T00:00:00Z',
    execute_after: null,
    executed_at: null,
    undo_until: null,
    undone_at: null
  } as unknown as CatalogMergeRequest
  const now = new Date('2026-10-21T00:00:00Z').getTime()

  it('waits on review while open', () => {
    const steps = mergeTimeline(base, now)
    expect(steps.map((s) => s.state)).toEqual([
      'done',
      'current',
      'upcoming',
      'upcoming'
    ])
  })

  it('counts down the cooling-off, then waits to execute', () => {
    const cooling = mergeTimeline(
      { ...base, state: 'approved', execute_after: '2026-10-22T00:00:00Z' },
      now
    )
    expect(cooling[2]!.state).toBe('current')
    const due = mergeTimeline(
      { ...base, state: 'approved', execute_after: '2026-10-20T12:00:00Z' },
      now
    )
    expect(due[2]!.state).toBe('done')
    expect(due[3]!.state).toBe('current')
  })

  it('offers undo after execution and records it once done', () => {
    const executed = mergeTimeline(
      {
        ...base,
        state: 'executed',
        executed_at: '2026-10-20T13:00:00Z',
        undo_until: '2026-11-19T13:00:00Z'
      },
      now
    )
    expect(executed.at(-1)).toMatchObject({ key: 'undo', state: 'current' })
    const undone = mergeTimeline(
      { ...base, state: 'undone', undone_at: '2026-10-20T14:00:00Z' },
      now
    )
    expect(undone.at(-1)).toMatchObject({ key: 'undo', state: 'stopped' })
  })

  it('stops after a rejection', () => {
    const steps = mergeTimeline({ ...base, state: 'rejected' }, now)
    expect(steps.slice(1).map((s) => s.state)).toEqual([
      'stopped',
      'stopped',
      'stopped'
    ])
  })
})

describe('noteEntries', () => {
  it('splits the reason from the decision lines', async () => {
    const { noteEntries } = await import('../app/utils/merge/timeline')
    expect(
      noteEntries(
        'VNDB 把它列为 v4302 的发行\n第二行\napproved by user 7\nchoices amended by user 7\nundone by user 9: 不是同一部'
      )
    ).toEqual([
      { kind: 'reason', uid: null, text: 'VNDB 把它列为 v4302 的发行\n第二行' },
      { kind: 'approved', uid: '7', text: '' },
      { kind: 'amended', uid: '7', text: '' },
      { kind: 'undone', uid: '9', text: '不是同一部' }
    ])
  })
})
