export type EditorApplicationStatus = 'pending' | 'approved' | 'declined'

export interface EditorApplication {
  id: number
  user_id: number
  status: EditorApplicationStatus
  reason: string
  reviewer_id?: number | null
  reviewed_at?: string | null
  decline_reason: string
  created_at: string
  updated_at: string
}

export interface EditorApplicationReply {
  data: EditorApplication | null
}

export interface AccountProblem {
  code: number
  message: string
}
