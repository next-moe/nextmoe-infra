export type DevAppReviewStatus = '' | 'approved' | 'pending' | 'declined'

export interface DevUserLogin {
  redirect_uris: string[]
  scopes: string[]
  pkce_required: boolean
}

export interface DevApp {
  client_id: string
  name: string
  description: string
  dev_enabled: boolean
  tier: string
  rate_per_min: number
  quota_daily: number
  key_count: number
  created_at: string
  review_status: DevAppReviewStatus
  review_note?: string
  user_login?: DevUserLogin | null
}

export type DevPolicyMode = 'self_service' | 'approval' | 'disabled'

// Keyed by capability (app.create / app.manage / key.mint).
export type DevPolicies = Record<string, DevPolicyMode>

export interface DevKey {
  id: number
  client_id: string
  name: string
  key_prefix: string
  last4: string
  scopes: string[]
  expires_at?: string
  revoked_at?: string
  last_used_at?: string
  created_at: string
}

export interface DevKeyMinted extends DevKey {
  key: string
}

export interface DevUsageDayFace {
  day: string
  face: string
  count: number
  status_4xx: number
  status_5xx: number
}

export interface DevUsageDay {
  day: string
  count: number
  status_4xx: number
  status_5xx: number
}

export interface DevUsageApp {
  client_id: string
  name: string
  count: number
  status_4xx: number
  status_5xx: number
}

export interface DevUsageFace {
  face: string
  count: number
  status_4xx: number
  status_5xx: number
}

// The shape the console's breakdown chart+table renders, normalised from the
// by-app and by-face slices so one component serves both.
export interface DevBreakdownRow {
  key: string
  label: string
  to?: string
  count: number
  status_4xx: number
  status_5xx: number
}

export interface DevLiveKey {
  app_name: string
  key_id: number
  rate_limit: number
  quota_limit: number
  quota_used: number
  quota_remaining: number
  quota_reset: number
}

export interface DevUsageSummary {
  days: number
  since: string
  total_count: number
  total_4xx: number
  total_5xx: number
  daily: DevUsageDay[]
  by_app: DevUsageApp[]
  by_face: DevUsageFace[]
  live: DevLiveKey[]
  live_unavailable?: boolean
}

export interface User {
  uuid: string
  name: string
  email: string
  avatar: string
  avatar_image_hash?: string | null
  bio?: string
  roles: string[]
  created_at: string
}
