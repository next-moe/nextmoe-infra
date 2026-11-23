export type CanonTodoKind = 'identity' | 'agent_on_human'

export type CanonTodoState = 'open' | 'done' | 'rejected'

export interface CanonCandidateValue {
  value: unknown
  tier?: string
}

export interface CanonNewEntity {
  ref: string
  entity: string
  properties: Record<string, CanonCandidateValue>
}

export interface CanonOutboxRow {
  kind: 'candidate' | 'changeset'
  program?: string
  candidate?: {
    target: { entity: string; id: number }
    property: string
    value: unknown
    tier?: string
  }
  changeset?: { lead_id: string; entities: CanonNewEntity[] }
}

export interface CanonTodo {
  id: string
  kind: CanonTodoKind
  subject_key: string
  state: CanonTodoState
  detail: string
  source: string
  suggestion: CanonOutboxRow
  resolved_by: string
  created_at: string
}

export interface CanonTodoList {
  items: CanonTodo[]
  next_cursor?: string
}

export interface CanonTodoResult {
  state: CanonTodoState
  ids?: Record<string, string>
}
