export type OAuthClientKind = 'site' | 'service' | 'developer'

export const oauthClientKind = (c: OAuthClient): OAuthClientKind => {
  if (c.site_id) return 'site'
  return c.owner_user_id ? 'developer' : 'service'
}

export const oauthClientHasStorage = (c: OAuthClient) =>
  Boolean(c.storage?.image_enabled || c.storage?.artifact_enabled)

export const oauthClientMatches = (
  c: OAuthClient,
  siteName: string,
  q: string
) =>
  [c.name, c.id, siteName, ...(c.redirect_uris ?? [])].some((v) =>
    v.toLowerCase().includes(q)
  )
