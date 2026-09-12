declare module 'nuxt/schema' {
  interface RuntimeConfig {
    apiBaseSsr: string
    catalogApiBaseSsr: string
    trustApiBaseSsr: string
    aiApiBaseSsr: string
    oauthClientSecret: string
  }

  interface PublicRuntimeConfig {
    apiBase: string
    catalogApiBase: string
    trustApiBase: string
    aiApiBase: string
    imageCdnBase: string
    oauthAuthorizeBase: string
    oauthWebBase: string
    oauthClientId: string
    oauthRedirectUri: string
  }
}

export {}
