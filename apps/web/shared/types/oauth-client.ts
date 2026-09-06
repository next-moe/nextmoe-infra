export interface OAuthClient {
  id: string
  site_id?: number
  name: string
  redirect_uris: string[]
  grants: string[]
  allowed_scopes?: string[]
  is_public?: boolean
  // page silently skips the user consent UI for already-logged-in users
  auto_consent?: boolean
  refresh_token_ttl_seconds?: number
  listed?: boolean
  logo_url?: string
  tagline?: string
  display_order?: number
  created_at: string
  storage?: OAuthClientStorageConfig
}

export interface OAuthClientStorageConfig {
  artifact_enabled: boolean
  artifact_site_key: string
  artifact_cdn_base: string
  artifact_allowed_mime: string[]
  artifact_max_file_size: number
  artifact_quota_daily: number
  artifact_quota_bytes_daily: number
  image_enabled: boolean
  image_site_key: string
  image_cdn_base: string
  image_allowed_presets: string[]
  image_max_file_size: number
  image_quota_daily: number
  image_quota_bytes_daily: number
}

export const DEFAULT_REFRESH_TOKEN_TTL_SECONDS = 60 * 60 * 24 * 90

export interface OAuthClientCreated extends OAuthClient {
  secret: string
}

export interface EcosystemApp {
  name: string
  site_domain: string
  logo_url?: string
  tagline?: string
  auto_consent: boolean
}

export const ALL_GRANTS = ['authorization_code', 'refresh_token'] as const

// The client editor's checkboxes and the consent screen's wording are the same
// registry. When they were two lists, playtime:read/playtime:write existed only
// in the consent one, so the forum client could not be granted them from the
// console at all: /oauth/authorize answered 15006 and the feature stayed dark.
export const SCOPE_LABELS: Record<string, string> = {
  openid: '身份标识',
  profile: '用户资料 (昵称、头像)',
  email: '邮箱地址',
  'playtime:read': '读取你的游戏时长记录',
  'playtime:write': '记录你的游戏时长',
  'catalog:edit': '以你的名义提交目录条目的编辑提案',
  'catalog:read': '以你的名义读取公开目录数据 (计入你的每日配额)',
  'image:upload': '以你的名义上传图片',
  'artifact:upload': '以你的名义上传文件',
}

export const KNOWN_SCOPES: readonly string[] = Object.keys(SCOPE_LABELS)

export const REN_ONLY_SCOPES: readonly string[] = ['image:upload', 'artifact:upload']
