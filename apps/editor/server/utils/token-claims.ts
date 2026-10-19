export interface AccessTokenClaims {
  sub: string
  id: number
  name: string
  scope: string
  roles: string[]
  siteRoles: string[]
  exp: number
}

const asStrings = (value: unknown) =>
  Array.isArray(value)
    ? value.filter((v): v is string => typeof v === 'string')
    : []

export const decodeAccessToken = (token: string): AccessTokenClaims | null => {
  const payload = token.split('.')[1]
  if (!payload) {
    return null
  }
  try {
    const raw = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'))
    if (
      typeof raw !== 'object' ||
      raw === null ||
      typeof raw.exp !== 'number'
    ) {
      return null
    }
    return {
      sub: typeof raw.sub === 'string' ? raw.sub : '',
      id: typeof raw.id === 'number' ? raw.id : 0,
      name: typeof raw.name === 'string' ? raw.name : '',
      scope: typeof raw.scope === 'string' ? raw.scope : '',
      roles: asStrings(raw.roles),
      siteRoles: asStrings(raw.site_roles),
      exp: raw.exp
    }
  } catch {
    return null
  }
}

export const ACCESS_REFRESH_SKEW_SECONDS = 60

export const isAccessTokenFresh = (
  claims: AccessTokenClaims | null,
  nowSeconds: number
): claims is AccessTokenClaims =>
  claims !== null && claims.exp - ACCESS_REFRESH_SKEW_SECONDS > nowSeconds
