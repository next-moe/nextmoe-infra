// Hikarinagi's brand guide names the account system "Hikarinagi ID" for login and
// authorization wording, and sanctions plain text wherever their mark cannot be
// shown at a legible size — we ship no mark file, so the label carries the entry.
export const FEDERATION_PROVIDER_LABEL: Record<string, string> = {
  google: 'Google',
  github: 'GitHub',
  hikarinagi: 'Hikarinagi ID'
}

export const federationLabel = (provider: string) =>
  FEDERATION_PROVIDER_LABEL[provider] ?? '第三方'
