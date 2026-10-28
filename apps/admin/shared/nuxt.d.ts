declare module 'nuxt/schema' {
  interface RuntimeConfig {
    apiBaseSsr: string
    catalogApiBaseSsr: string
    trustApiBaseSsr: string
    aiApiBaseSsr: string
    agentApiBaseSsr: string
    oauthClientSecret: string
  }

  interface PublicRuntimeConfig {
    apiBase: string
    catalogApiBase: string
    trustApiBase: string
    aiApiBase: string
    agentApiBase: string
    imageCdnBase: string
    oauthAuthorizeBase: string
    oauthWebBase: string
    oauthClientId: string
    oauthRedirectUri: string
  }
}

export {}
