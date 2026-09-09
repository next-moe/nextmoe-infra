
export type PermissionGrantSource = 'none' | 'code' | 'grant' | 'deny'

export interface PermissionCell {
  granted: boolean
  source: PermissionGrantSource
  editable: boolean
  can_deny: boolean
  can_restore: boolean
  reason?: string
}

export interface PermissionKeyRow {
  key: string
  desc_en: string
  desc_zh: string
  non_delegable: boolean
  grants: Record<string, PermissionCell>
}

export interface PermissionDomainView {
  name: string
  title_zh: string
  keys: PermissionKeyRow[]
}

export interface PermissionMatrix {
  roles: string[]
  editable_roles: string[]
  manages_permissions: boolean
  domains: PermissionDomainView[]
}

export type PermissionAuditAction = 'grant' | 'revoke' | 'deny' | 'revoke_deny'

export type PermissionEffect = 'grant' | 'deny'

export interface PermissionAuditEntry {
  id: number
  actor_user_id: number
  actor_name: string
  action: PermissionAuditAction
  role: string
  permission: string
  created_at: string
}
