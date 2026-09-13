export interface NewsVerdict {
  id: number
  content_fingerprint: string
  tier0_decision: string
  tier0_matched?: string[]
  // ai_flagged is nullable on the wire on purpose: null means the model never
  // spoke, and rendering that as "clean" would undo the whole gate.
  ai_flagged: boolean | null
  ai_score: number | null
  ai_categories?: string[]
  ai_channel?: string
  degraded: boolean
  degraded_reason?: string
  created_at: string
  current: boolean
}

export interface NewsDecision {
  id: number
  actor_uid: number
  from_status: number
  to_status: number
  reason?: string
  created_at: string
}

export interface NewsAdminItem {
  id: number
  source_key: string
  lane: string
  upstream_category?: string
  external_id: string
  title: string
  preview: string
  source_url: string
  banner_url?: string
  published_at: string
  status: number
  dead_at: string | null
  first_seen_at: string
  last_seen_at: string
  verdict: NewsVerdict | null
  attempts: number
}

export interface NewsAdminItemDetail extends NewsAdminItem {
  verdicts: NewsVerdict[]
  decisions: NewsDecision[]
}

export interface NewsAdminQueue {
  items: NewsAdminItem[]
  total: number
}

export interface NewsAdminStats {
  by_status: Record<string, number>
  by_lane: Record<string, number>
  ungraded: number
  degraded: number
}
