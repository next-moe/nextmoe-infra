export interface ConsentClaim {
  key: 'picture' | 'name' | 'email' | 'sub' | 'roles'
  field: string
  label: string
  scope?: string
}

/*
 * The consent screen states this list is exactly what /oauth/userinfo hands
 * over, so it has to follow dto.UserInfoResponse rather than the scope names:
 * id, sub, roles and site_roles are filled on every request whatever the scope
 * was, and only name/picture (profile) and email (email) are actually gated.
 * A list keyed purely on scopes would have understated what a client holding
 * nothing but openid receives.
 */
export const CONSENT_CLAIMS: ConsentClaim[] = [
  { key: 'picture', field: 'picture', label: '头像', scope: 'profile' },
  { key: 'name', field: 'name', label: '昵称', scope: 'profile' },
  { key: 'email', field: 'email', label: '邮箱', scope: 'email' },
  { key: 'sub', field: 'sub · id', label: '账号标识' },
  { key: 'roles', field: 'roles · site_roles', label: '账号角色' }
]

export const IDENTITY_SCOPES = ['openid', 'profile', 'email']
