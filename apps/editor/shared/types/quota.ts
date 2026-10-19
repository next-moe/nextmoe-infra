export interface EditQuotaCounter {
  limit: number | null
  used: number
  remaining: number | null
}

export type EditQuotaTier = 'user' | 'editor' | 'reviewer'

export interface EditQuota {
  object: 'edit_quota'
  quota_tier: EditQuotaTier
  window_hours: number
  proposals: EditQuotaCounter
  direct_edits: EditQuotaCounter
  open_proposals: EditQuotaCounter
}
