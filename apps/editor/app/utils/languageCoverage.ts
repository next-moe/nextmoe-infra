export const COMMON_TEXT_LANGS = ['ja', 'zh-Hans', 'zh-Hant', 'en']

const orderLangs = (olang: string | null | undefined, langs: string[]) => {
  const seen = new Set<string>()
  const out: string[] = []
  const push = (lang: string) => {
    if (!seen.has(lang)) {
      seen.add(lang)
      out.push(lang)
    }
  }
  if (olang && langs.includes(olang)) {
    push(olang)
  }
  for (const lang of COMMON_TEXT_LANGS) {
    if (langs.includes(lang)) {
      push(lang)
    }
  }
  for (const lang of [...langs].sort()) {
    if (lang) {
      push(lang)
    }
  }
  if (langs.includes('')) {
    push('')
  }
  return out
}

export type IntroLanguageStatus = 'human' | 'upstream' | 'missing'

export interface IntroUpstreamInput {
  lang: string
  hidden: boolean
  source?: string
  machine?: boolean
}

export interface IntroLanguage {
  lang: string
  status: IntroLanguageStatus
  upstreamCount: number
  hiddenCount: number
  displaySource?: string
  machine: boolean
  isOriginal: boolean
}

export const introLanguages = (input: {
  olang?: string | null
  human: { lang: string }[]
  upstream: IntroUpstreamInput[]
  extra?: string[]
}): IntroLanguage[] => {
  const humanLangs = new Set(input.human.map((row) => row.lang))
  const langs = orderLangs(input.olang, [
    ...(input.olang ? [input.olang] : []),
    ...COMMON_TEXT_LANGS,
    ...humanLangs,
    ...input.upstream.map((row) => row.lang),
    ...(input.extra ?? [])
  ])
  return langs
    .filter((lang) => lang !== '')
    .map((lang) => {
      const versions = input.upstream.filter((row) => row.lang === lang)
      const visible = versions.filter((row) => !row.hidden)
      const shown = visible[0]
      const human = humanLangs.has(lang)
      return {
        lang,
        status: human ? 'human' : shown ? 'upstream' : 'missing',
        upstreamCount: visible.length,
        hiddenCount: versions.length - visible.length,
        displaySource: human ? undefined : shown?.source,
        machine: !human && Boolean(shown?.machine),
        isOriginal: lang === input.olang
      }
    })
}

export interface TitleLanguage {
  lang: string
  count: number
  official: boolean
  missing: boolean
  isOriginal: boolean
}

export const titleLanguages = (input: {
  olang?: string | null
  titles: { lang?: unknown; kind?: unknown }[]
  officialKind?: number
}): TitleLanguage[] => {
  const officialKind = input.officialKind ?? 0
  const byLang = new Map<string, { count: number; official: boolean }>()
  for (const row of input.titles) {
    const lang = typeof row.lang === 'string' ? row.lang : ''
    const entry = byLang.get(lang) ?? { count: 0, official: false }
    entry.count++
    if (Number(row.kind) === officialKind) {
      entry.official = true
    }
    byLang.set(lang, entry)
  }
  const langs = orderLangs(input.olang, [
    ...(input.olang ? [input.olang] : []),
    ...COMMON_TEXT_LANGS,
    ...byLang.keys()
  ])
  return langs.map((lang) => {
    const entry = byLang.get(lang)
    return {
      lang,
      count: entry?.count ?? 0,
      official: entry?.official ?? false,
      missing: !entry,
      isOriginal: lang === input.olang
    }
  })
}
