export type CanonTodoKind =
  | 'identity'
  | 'agent_on_human'
  | 'first_write'
  | 'conflict'

export type CanonTodoState = 'open' | 'done' | 'rejected' | 'superseded'

export interface CanonTodo {
  id: string
  kind: CanonTodoKind
  subject_key: string
  program: string
  state: CanonTodoState
  detail: string
  source: string
  suggestion: CanonSuggestion
  questions: { ref: string; entity: string; name: string }[]
  resolved_by: string
  created_at: string
}

/** The pool row as it arrived; only what the list line names is typed. */
export interface CanonSuggestion {
  candidate?: {
    target: { entity: string; id: number }
    property: string
    value: unknown
  }
  changeset?: {
    entities: {
      ref: string
      entity: string
      properties: Record<string, { value: unknown }>
    }[]
  }
}

export interface CanonTodoList {
  items: CanonTodo[]
  next_cursor?: string
}

export interface CanonTodoResult {
  state: CanonTodoState
  ids?: Record<string, string>
}

export interface CanonSheetValue {
  property: string
  value: unknown
  is?: string
  tier?: string
  evidence?: string[]
}

export interface CanonSheetChange {
  ref: string
  entity: string
  id?: number
  values: CanonSheetValue[] | null
}

export interface CanonSheetCandidate {
  token: string
  id: number
  label: string
  facts: string[]
}

export interface CanonSheetQuestion {
  ref: string
  entity: string
  name: string
  why: string
  cites: boolean
  candidates: CanonSheetCandidate[]
}

export interface CanonTodoSheet {
  todo_id: number
  kind: CanonTodoKind
  program: string
  detail: string
  changes: CanonSheetChange[]
  questions: CanonSheetQuestion[]
  notes?: { by: string; note: string; at: string }[]
}

export interface CanonTodoAnswer {
  use?: string
  adopt?: string
  new?: boolean
}

export interface CanonSchemaProperty {
  key: string
  shape: string
  vocab?: string
  ref?: string
  doc: string
  clauses: string[]
  agent_may: boolean
  immutable: boolean
}

export interface CanonSchemaEntity {
  name: string
  properties: CanonSchemaProperty[]
  children: Record<string, string>
}

export interface CanonSchemas {
  entities: CanonSchemaEntity[]
  vocabularies: { name: string; terms: { key: string; doc?: string }[] }[]
}

export interface CanonFieldState {
  by: 'agent' | 'human' | 'system'
  key: string
  tier: string
  source: string
  reviewed_by: string
  at: string
  evidence: string[]
}

export interface CanonRecord {
  entity: string
  id: string
  version: number
  values: Record<string, unknown>
  state: Record<string, CanonFieldState>
  called: string
  is: Record<string, string>
  held: boolean
  children?: Record<string, CanonRecord[]>
}

export interface CanonEditChange {
  ref?: string
  entity: string
  id?: string
  expect_version?: number
  values?: Record<string, unknown>
  remove?: boolean
  distinct?: string[]
}

export interface CanonProblemCandidate {
  token: string
  id: string
  label: string
}

export interface CanonRevision {
  id: string
  entity: string
  entity_id: string
  action: string
  actor_kind: string
  actor_key: string
  reviewed_by: string
  batch: string
  note: string
  changes: Record<string, unknown>
  undone: boolean
  at: string
}

export interface CanonPatrolItem {
  batch: string
  program: string
  reviewed_by: string
  at: string
  revisions: CanonRevision[]
}

export interface CanonProgram {
  key: string
  medium: string
  doc: string
  mode: 'review' | 'auto' | 'stopped'
  set_by: string
  note: string
  set_at: string | null
  todos: Record<string, number>
  commits: number
  undone: number
  unpatrolled: number
  last_write: string | null
}

export interface CanonPage<T> {
  items: T[]
  next_cursor?: string
}

export interface CanonWorkHit {
  id: string
  medium: string
  titles: { text: string; lang: string; kind: string }[]
}
