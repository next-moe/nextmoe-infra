// Hikarinagi's brand guide names the account system "Hikarinagi ID" for login
// and authorization wording; Google and GitHub ship as inline SVG in
// FederationButtons because their marks are single-path and theme-neutral.
export const FEDERATION_PROVIDER_LABEL: Record<string, string> = {
  google: 'Google',
  github: 'GitHub',
  hikarinagi: 'Hikarinagi ID'
}

export const FEDERATION_PROVIDER_MARK: Record<string, string> = {
  hikarinagi: '/federation/hikarinagi.webp'
}

export const federationLabel = (provider: string) =>
  FEDERATION_PROVIDER_LABEL[provider] ?? '第三方'
