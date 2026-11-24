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
