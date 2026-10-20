import { describe, expect, it, vi } from 'vitest'
import type { ActivityProposal } from '../shared/types/activity'
import type { EditQuotaCounter } from '../shared/types/quota'
import {
  accountUpstreamPath,
  forwardAccountRequest,
  mapAccountResponse,
  sanitizeApplicationBody
} from '../server/utils/account-proxy'
import { passesCsrfCheck } from '../server/utils/proxy-policy'
import {
  quotaCounterLabel,
  quotaRemainingText,
  quotaTone,
  quotaUsedRatio
} from '../app/utils/quotaFormat'
import {
  draftEditPath,
  parseLocalDraft,
  readLocalDrafts
} from '../app/utils/localDrafts'
import { latestNoticeTime, proposalNotices } from '../app/utils/proposalNotices'
import { weeklyActivity } from '../app/utils/contributionStats'

describe('editor application proxy', () => {
  it('only forwards the two application routes', () => {
    expect(accountUpstreamPath('GET')).toBe('/editor/applications/me')
    expect(accountUpstreamPath('post')).toBe('/editor/applications')
    expect(accountUpstreamPath('PUT')).toBeNull()
    expect(accountUpstreamPath('DELETE')).toBeNull()
    expect(accountUpstreamPath('PATCH')).toBeNull()
  })

  it('keeps only a trimmed reason within 10–1000 characters', () => {
    expect(
      sanitizeApplicationBody({
        reason: '  我常编辑作品的职员表和标签  ',
        admin: true
      })
    ).toEqual({
      reason: '我常编辑作品的职员表和标签'
    })
    expect(sanitizeApplicationBody({ reason: '太短了' })).toBeNull()
    expect(sanitizeApplicationBody({ reason: '字'.repeat(1001) })).toBeNull()
    expect(sanitizeApplicationBody({ reason: '字'.repeat(1000) })).toEqual({
      reason: '字'.repeat(1000)
    })
    expect(sanitizeApplicationBody({ reason: 42 })).toBeNull()
    expect(sanitizeApplicationBody(null)).toBeNull()
    expect(sanitizeApplicationBody('reason')).toBeNull()
  })

  it('counts code points, not UTF-16 units, toward the minimum', () => {
    expect(sanitizeApplicationBody({ reason: '😀'.repeat(9) })).toBeNull()
    expect(sanitizeApplicationBody({ reason: '😀'.repeat(10) })).not.toBeNull()
  })

  it('needs the CSRF header on a submit', () => {
    expect(
      passesCsrfCheck({ method: 'POST', host: 'editor.nextmoe.com' })
    ).toBe(false)
    expect(
      passesCsrfCheck({
        method: 'POST',
        csrfHeader: '1',
        origin: 'https://evil.example',
        host: 'editor.nextmoe.com'
      })
    ).toBe(false)
    expect(
      passesCsrfCheck({
        method: 'POST',
        csrfHeader: '1',
        origin: 'https://editor.nextmoe.com',
        host: 'editor.nextmoe.com'
      })
    ).toBe(true)
  })

  it('sends the bearer token and body to the oauth face', async () => {
    const fetchImpl = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            code: 0,
            message: '成功',
            data: { id: 3, status: 'pending' }
          }),
          { status: 200 }
        )
    )
    const result = await forwardAccountRequest({
      fetchImpl: fetchImpl as unknown as typeof fetch,
      base: 'http://oauth:9277/api/v1',
      token: 'tok',
      method: 'POST',
      body: { reason: '我常编辑作品的职员表和标签' }
    })
    expect(fetchImpl).toHaveBeenCalledOnce()
    const [url, init] = fetchImpl.mock.calls[0] as unknown as [
      string,
      RequestInit
    ]
    expect(url).toBe('http://oauth:9277/api/v1/editor/applications')
    expect(init.method).toBe('POST')
    expect((init.headers as Record<string, string>).authorization).toBe(
      'Bearer tok'
    )
    expect(JSON.parse(String(init.body))).toEqual({
      reason: '我常编辑作品的职员表和标签'
    })
    expect(result).toEqual({
      status: 200,
      body: { data: { id: 3, status: 'pending' } },
      sessionExpired: false
    })
  })

  it('reads the status with a bodiless GET', async () => {
    const fetchImpl = vi.fn(
      async () => new Response(JSON.stringify({ code: 0, message: '成功' }))
    )
    const result = await forwardAccountRequest({
      fetchImpl: fetchImpl as unknown as typeof fetch,
      base: 'http://oauth:9277/api/v1',
      token: 'tok',
      method: 'GET'
    })
    const [url, init] = fetchImpl.mock.calls[0] as unknown as [
      string,
      RequestInit
    ]
    expect(url).toBe('http://oauth:9277/api/v1/editor/applications/me')
    expect(init.body).toBeUndefined()
    expect(result.body).toEqual({ data: null })
  })

  it('refuses a method it does not proxy without calling upstream', async () => {
    const fetchImpl = vi.fn()
    const result = await forwardAccountRequest({
      fetchImpl: fetchImpl as unknown as typeof fetch,
      base: 'http://oauth:9277/api/v1',
      token: 'tok',
      method: 'DELETE'
    })
    expect(fetchImpl).not.toHaveBeenCalled()
    expect(result.status).toBe(405)
  })

  it('maps the oauth envelope onto editor responses', () => {
    expect(
      mapAccountResponse(400, {
        code: 17012,
        message: '已有一份待审核的编辑者申请'
      })
    ).toEqual({
      status: 400,
      body: { code: 17012, message: '已有一份待审核的编辑者申请' },
      sessionExpired: false
    })
    expect(mapAccountResponse(401, { code: 10005 }).sessionExpired).toBe(true)
    expect(mapAccountResponse(500, { code: 1 }).status).toBe(502)
    expect(mapAccountResponse(200, { code: 17013, message: 'x' }).status).toBe(
      502
    )
    expect(
      mapAccountResponse(403, { code: 10010, message: '账号已封禁' })
    ).toEqual({
      status: 403,
      body: { code: 10010, message: '账号已封禁' },
      sessionExpired: false
    })
  })

  it('reports an unreachable oauth service as 502', async () => {
    const result = await forwardAccountRequest({
      fetchImpl: (async () => {
        throw new Error('ECONNREFUSED')
      }) as unknown as typeof fetch,
      base: 'http://oauth:9277/api/v1',
      token: 'tok',
      method: 'GET'
    })
    expect(result.status).toBe(502)
  })
})

describe('quota formatting', () => {
  const counter = (limit: number | null, used: number): EditQuotaCounter => ({
    limit,
    used,
    remaining: limit === null ? null : Math.max(limit - used, 0)
  })

  it('labels unlimited, unavailable and counted lanes', () => {
    expect(quotaCounterLabel(counter(null, 3))).toBe('3 / 不限')
    expect(quotaCounterLabel(counter(0, 0))).toBe('不可用')
    expect(quotaCounterLabel(counter(100, 12))).toBe('12 / 100')
    expect(quotaRemainingText(counter(100, 12))).toBe('剩 88')
    expect(quotaRemainingText(counter(null, 1))).toBe('不限')
  })

  it('turns warning near the limit and danger when spent', () => {
    expect(quotaTone(counter(100, 10))).toBe('primary')
    expect(quotaTone(counter(100, 85))).toBe('warning')
    expect(quotaTone(counter(100, 100))).toBe('danger')
    expect(quotaTone(counter(0, 0))).toBe('default')
    expect(quotaUsedRatio(counter(10, 20))).toBe(1)
    expect(quotaUsedRatio(counter(null, 20))).toBe(0)
  })
})

describe('local drafts', () => {
  const draft = (savedAt: string, values: object) =>
    JSON.stringify({ savedAt, baseFingerprint: 'x', values })

  it('parses the work editor autosave', () => {
    expect(
      parseLocalDraft(
        'nextmoe-editor:draft:work:4',
        draft('2026-10-19T10:00:00.000Z', {
          'catalog.work.titles': [],
          'catalog.work.olang': 'ja'
        })
      )
    ).toEqual({
      key: 'nextmoe-editor:draft:work:4',
      family: 'work',
      entityId: '4',
      savedAt: '2026-10-19T10:00:00.000Z',
      fieldKeys: ['catalog.work.titles', 'catalog.work.olang']
    })
  })

  it.each([
    [
      'nextmoe-editor-sidebar-collapsed',
      draft('2026-10-19T10:00:00Z', { a: 1 })
    ],
    ['nextmoe-editor:draft:work:4', '{not json'],
    ['nextmoe-editor:draft:work:4', draft('2026-10-19T10:00:00Z', {})],
    ['nextmoe-editor:draft:work:4', draft('yesterday', { a: 1 })],
    [
      'nextmoe-editor:draft:work:4',
      JSON.stringify({ savedAt: '2026-10-19T10:00:00Z', values: [1] })
    ],
    ['nextmoe-editor:draft:work:4', null]
  ])('ignores %s / malformed value', (key, raw) => {
    expect(parseLocalDraft(key, raw)).toBeNull()
  })

  it('lists drafts newest first and skips unrelated keys', () => {
    const entries: [string, string][] = [
      ['nextmoe-editor:draft:work:1', draft('2026-10-18T10:00:00Z', { a: 1 })],
      ['nextmoe-editor-color-mode', 'dark'],
      [
        'nextmoe-editor:draft:character:9',
        draft('2026-10-19T10:00:00Z', { b: 1 })
      ]
    ]
    const storage = {
      length: entries.length,
      key: (index: number) => entries[index]?.[0] ?? null,
      getItem: (key: string) => entries.find(([k]) => k === key)?.[1] ?? null
    }
    expect(readLocalDrafts(storage).map((d) => d.key)).toEqual([
      'nextmoe-editor:draft:character:9',
      'nextmoe-editor:draft:work:1'
    ])
  })

  it('survives a storage that throws', () => {
    const storage = {
      get length(): number {
        throw new Error('SecurityError')
      },
      key: () => null,
      getItem: () => null
    }
    expect(readLocalDrafts(storage)).toEqual([])
  })

  it('links a draft back to its editor when the family has one', () => {
    expect(draftEditPath({ family: 'work', entityId: '4' })).toBe(
      '/works/4/edit'
    )
    expect(
      draftEditPath({ family: 'unknown_family', entityId: '4' })
    ).toBeNull()
  })
})

describe('proposal notices', () => {
  const proposal = (overrides: Partial<ActivityProposal>): ActivityProposal =>
    ({
      object: 'proposal',
      id: '1',
      state: 'open',
      target_object: 'work',
      entity_type: 'catalog.work',
      entity_id: '4',
      proposer_uid: '7',
      site: 'editor',
      base_revision_seq: 1,
      note: '',
      created_at: '2026-10-19T08:00:00Z',
      updated_at: '2026-10-19T08:00:00Z',
      ...overrides
    }) as ActivityProposal

  const seen = '2026-10-19T09:00:00Z'

  it('reports decisions made after the last visit, newest first', () => {
    const notices = proposalNotices(
      [
        proposal({
          id: '1',
          state: 'merged',
          decided_at: '2026-10-19T09:30:00Z'
        }),
        proposal({
          id: '2',
          state: 'declined',
          decided_at: '2026-10-19T10:00:00Z'
        }),
        proposal({
          id: '3',
          state: 'merged',
          decided_at: '2026-10-19T08:30:00Z'
        }),
        proposal({
          id: '4',
          state: 'withdrawn',
          decided_at: '2026-10-19T10:00:00Z'
        })
      ],
      seen
    )
    expect(notices.map((n) => [n.proposal.id, n.kind])).toEqual([
      ['2', 'declined'],
      ['1', 'merged']
    ])
  })

  it('reports an open proposal someone touched after it was filed', () => {
    const notices = proposalNotices(
      [
        proposal({ id: '5', updated_at: '2026-10-19T09:10:00Z' }),
        proposal({
          id: '6',
          created_at: '2026-10-19T09:20:00Z',
          updated_at: '2026-10-19T09:20:00Z'
        })
      ],
      seen
    )
    expect(notices.map((n) => [n.proposal.id, n.kind])).toEqual([
      ['5', 'updated']
    ])
  })

  it('treats a missing watermark as "everything is new"', () => {
    expect(
      proposalNotices(
        [proposal({ state: 'merged', decided_at: '2020-01-01T00:00:00Z' })],
        null
      )
    ).toHaveLength(1)
  })

  it('advances the watermark to the newest notice', () => {
    const notices = proposalNotices(
      [
        proposal({
          id: '1',
          state: 'merged',
          decided_at: '2026-10-19T09:30:00Z'
        }),
        proposal({
          id: '2',
          state: 'declined',
          decided_at: '2026-10-19T10:00:00Z'
        })
      ],
      seen
    )
    expect(latestNoticeTime(notices)).toBe('2026-10-19T10:00:00Z')
    expect(latestNoticeTime([])).toBeNull()
  })
})

describe('weekly activity', () => {
  it('buckets timestamps into the last N weeks and drops older ones', () => {
    const now = Date.parse('2026-10-21T12:00:00')
    const weeks = weeklyActivity(
      [
        '2026-10-20T08:00:00',
        '2026-10-21T09:00:00',
        '2026-10-13T09:00:00',
        '2026-01-01T00:00:00',
        null,
        'not a date'
      ],
      4,
      now
    )
    expect(weeks).toHaveLength(4)
    expect(weeks.map((week) => week.count)).toEqual([0, 0, 1, 2])
  })
})
