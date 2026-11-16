export type SettingKind =
  'bool' | 'int' | 'float' | 'string' | 'enum' | 'string_list'

export type SettingSource = 'db' | 'default' | 'site'

export type SettingValue = boolean | number | string | string[]

export interface SettingsOverrideView {
  value: SettingValue
  version: number
  updated_by_user_id: number
  updated_by_name: string
  note: string
  updated_at: string
}

export interface SettingsKeyView {
  key: string
  kind: SettingKind
  desc_en: string
  desc_zh: string
  default: SettingValue
  env_var?: string
  enum?: string[]
  min?: number
  max?: number
  pattern?: string
  multiline?: boolean
  effective: SettingValue
  source: SettingSource
  override: SettingsOverrideView | null
  site_scoped: boolean
  public: boolean
  inherited?: SettingValue
}

export interface SettingsDomainView {
  name: string
  title_zh: string
  keys: SettingsKeyView[]
}

export interface SettingsOverview {
  scope: { kind: 'platform' | 'site'; site_id?: number }
  domains: SettingsDomainView[]
  writable: boolean
}

export type SettingsAuditAction = 'set' | 'reset'

export interface SettingsAuditEntry {
  id: number
  actor_user_id: number
  actor_name: string
  action: SettingsAuditAction
  key: string
  old_value: SettingValue | null
  new_value: SettingValue | null
  note: string
  created_at: string
  scope_kind: 'platform' | 'site'
  scope_id: string
}

export type EnvSource = 'env' | 'store' | 'default'

export type EnvVarKind = 'bootstrap' | 'topology' | 'panel' | 'storable'

export type EnvIssue =
  'env_redundant' | 'env_shadows' | 'restart_pending' | 'kek_missing'

export interface EnvServiceView {
  name: string
  hostname: string
  started_at: string
  store_loaded: boolean
  unread: string[]
}

export interface EnvServiceValue {
  service: string
  source: EnvSource
  set: boolean
  value?: string
  fingerprint?: string
  issue?: EnvIssue
}

export interface EnvStoredView {
  scope: string
  value?: string
  hint?: string
  fingerprint: string
  version: number
  updated_by_name: string
  note: string
  updated_at: string
}

export interface EnvVarView {
  key: string
  secret: boolean
  kind: EnvVarKind
  default?: string
  services: EnvServiceValue[]
  stored: EnvStoredView[]
  issues: EnvIssue[]
  diverged: boolean
}

export interface EnvOverview {
  writable: boolean
  kek_configured: boolean
  scopes: string[]
  services: EnvServiceView[]
  vars: EnvVarView[]
}

export type EnvAuditAction = 'set' | 'reset' | 'import'

export interface EnvAuditEntry {
  id: number
  actor_user_id: number
  actor_name: string
  action: EnvAuditAction
  scope: string
  key: string
  old_fingerprint: string
  new_fingerprint: string
  old_value: string
  new_value: string
  note: string
  created_at: string
}
