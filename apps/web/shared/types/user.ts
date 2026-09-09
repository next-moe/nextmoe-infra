export interface User {
  id?: number
  uuid: string
  name: string
  email: string
  avatar: string                  // legacy URL string (kungal/moyu old WebP)
  avatar_image_hash?: string | null  // image_service hash, set on new uploads
  bio: string
  moemoepoint: number
  status: number
  is_anonymized?: boolean // PII irreversibly scrubbed (terminal); shows 已注销
  original_email?: string
  roles: string[]
  created_at: string
}

export interface UserSiteData {
  site_id: number
  site_name: string
  status: number
}
export interface UserSiteRole {
  site_id: number
  site_name: string
  role_name: string
  granted_by: number
  granted_at: string
  expires_at?: string | null
  note?: string
}
export interface UserDetail extends User {
  ip: string
  session_count: number
  oauth_accounts: number
  site_data: UserSiteData[]
  site_roles: UserSiteRole[]
}

export interface LoginResponse {
  user: User
  access_token: string
}

export interface FederationProvidersResponse {
  providers: { name: string }[]
}

export interface FederationPendingResponse {
  provider: string
  suggested_name: string
  email: string
  email_locked: boolean
}

export interface RefreshResponse {
  access_token: string
}

export interface BagSession {
  sub: string
  name: string
  email: string
  avatar: string
  avatar_image_hash?: string | null
  roles: string[]
  active: boolean
  last_used_at: string
}
