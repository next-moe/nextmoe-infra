
export type DevAppReviewStatus = '' | 'approved' | 'pending' | 'declined'

export interface DevApp {
  client_id: string
  name: string
  owner_user_id?: number
  dev_enabled: boolean
  dev_tier: string
  dev_rate_per_min: number
  dev_quota_daily: number
  key_count: number
  review_status: DevAppReviewStatus
  review_note?: string
  archived_at?: string
  store_settlement_eligible: boolean
  login_client: boolean
  bound_site: boolean
  created_at: string
}

export type DevPolicyMode = 'self_service' | 'approval' | 'disabled'

export interface DevPolicy {
  key: string
  label_zh: string
  label_en: string
  desc_zh: string
  desc_en: string
  modes: DevPolicyMode[]
  default: DevPolicyMode
  mode: DevPolicyMode
  overridden: boolean
  set_by_user_id?: number
  updated_at?: string
}

export interface DevPolicyMatrix {
  editable: boolean
  policies: DevPolicy[]
}

export type DevKeyState = 'active' | 'revoked' | 'expired'

export interface DevAdminKey extends DevKey {
  app_name: string
  owner_user_id?: number
  state: DevKeyState
}

export interface DevAdminKeyList {
  items: DevAdminKey[]
  total: number
  page: number
  limit: number
}

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

