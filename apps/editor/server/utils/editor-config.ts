import type { H3Event } from 'h3'

const trimSlash = (value: string) => value.replace(/\/+$/, '')

export const editorServerConfig = (event: H3Event) => {
  const config = useRuntimeConfig(event)
  const authorizeBase = trimSlash(String(config.public.oauthAuthorizeBase))
  return {
    authorizeBase,
    tokenBase: trimSlash(String(config.oauthApiBaseSsr || authorizeBase)),
    accountWebBase: trimSlash(String(config.public.oauthWebBase)),
    catalogBase: trimSlash(String(config.catalogApiBaseSsr)),
    clientId: String(config.public.oauthClientId),
    redirectUri: String(config.public.oauthRedirectUri)
  }
}

export type EditorServerConfig = ReturnType<typeof editorServerConfig>
