import type { CatalogMergeRequest } from '#shared/types/catalog'

export type TimelineState = 'done' | 'current' | 'upcoming' | 'stopped'

export interface TimelineStep {
  key: 'filed' | 'review' | 'cooling' | 'executed' | 'undo'
  label: string
  state: TimelineState
  at: string | null
  detail: string
}

const time = (iso: string | null | undefined) =>
  iso ? new Date(iso).getTime() : null

export const mergeTimeline = (
  request: CatalogMergeRequest,
  now: number
): TimelineStep[] => {
  const state = request.state
  const after = time(request.execute_after)
  const undoUntil = time(request.undo_until)
  const ran = state === 'executed' || state === 'undone'
  const stopped = state === 'rejected' || state === 'withdrawn'

  const review: TimelineStep = {
    key: 'review',
    label: '审核',
    state: 'current',
    at: null,
    detail: '等待审核者确认'
  }
  if (state === 'rejected') {
    Object.assign(review, { state: 'stopped', detail: '已驳回' })
  } else if (state === 'withdrawn') {
    Object.assign(review, { state: 'stopped', detail: '提交者已撤回' })
  } else if (state !== 'open') {
    Object.assign(review, { state: 'done', detail: '已批准' })
  }

  const cooling: TimelineStep = {
    key: 'cooling',
    label: '冷静期',
    state: stopped ? 'stopped' : 'upcoming',
    at: request.execute_after ?? null,
    detail: '批准后等待一段时间，留给大家发现问题'
  }
  if (ran || (state === 'approved' && after !== null && after <= now)) {
    Object.assign(cooling, { state: 'done', detail: '冷静期已结束' })
  } else if (state === 'approved') {
    cooling.state = 'current'
  }

  const executed: TimelineStep = {
    key: 'executed',
    label: '执行合并',
    state: stopped ? 'stopped' : 'upcoming',
    at: request.executed_at ?? null,
    detail: '冷静期结束后自动执行，审核者也可以手动执行'
  }
  if (ran) {
    Object.assign(executed, { state: 'done', detail: '已合并' })
  } else if (state === 'approved' && after !== null && after <= now) {
    Object.assign(executed, { state: 'current', detail: '等待执行' })
  }

  const steps: TimelineStep[] = [
    {
      key: 'filed',
      label: '提交',
      state: 'done',
      at: request.proposed_at,
      detail: ''
    },
    review,
    cooling,
    executed
  ]
  if (state === 'undone') {
    steps.push({
      key: 'undo',
      label: '撤销合并',
      state: 'stopped',
      at: request.undone_at ?? null,
      detail: '审核者撤销了这次合并，被并入的条目已恢复'
    })
  } else if (state === 'executed' && undoUntil !== null) {
    steps.push({
      key: 'undo',
      label: '可撤销期',
      state: undoUntil > now ? 'current' : 'done',
      at: request.undo_until ?? null,
      detail: undoUntil > now ? '期间审核者可以撤销，恢复原编号' : '撤销期已过'
    })
  }
  return steps
}

export interface NoteEntry {
  kind:
    | 'reason'
    | 'approved'
    | 'rejected'
    | 'withdrawn'
    | 'amended'
    | 'undone'
    | 'system'
  uid: string | null
  text: string
}

const NOTE_LINE =
  /^(approved|rejected|withdrawn|choices amended|undone) by user (\d+)(?::\s*(.*))?$/

const NOTE_KIND: Record<string, NoteEntry['kind']> = {
  approved: 'approved',
  rejected: 'rejected',
  withdrawn: 'withdrawn',
  'choices amended': 'amended',
  undone: 'undone'
}

// The service appends one line per decision to the requester's reason:
// "approved by user 7", "rejected by user 7: <reason>", "unmerged into …".
export const noteEntries = (note: string): NoteEntry[] => {
  const entries: NoteEntry[] = []
  const reason: string[] = []
  for (const line of note.split('\n')) {
    const match = NOTE_LINE.exec(line.trim())
    if (match) {
      entries.push({
        kind: NOTE_KIND[match[1]!]!,
        uid: match[2]!,
        text: match[3] ?? ''
      })
    } else if (entries.length === 0) {
      reason.push(line)
    } else if (line.trim()) {
      entries.push({ kind: 'system', uid: null, text: line.trim() })
    }
  }
  const text = reason.join('\n').trim()
  return text ? [{ kind: 'reason', uid: null, text }, ...entries] : entries
}
