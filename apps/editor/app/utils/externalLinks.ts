export type LinkKind = 'identity' | 'probable' | 'related'

export const LINK_KIND_FROM_WIRE: LinkKind[] = [
  'identity',
  'probable',
  'related'
]

export const LINK_KIND_LABELS: Record<LinkKind, string> = {
  identity: '身份链接',
  probable: '待确认身份',
  related: '相关链接'
}

export const LINK_SOURCE_LABELS: Record<string, string> = {
  vndb: 'VNDB',
  bangumi: 'Bangumi',
  erogamescape: 'ErogameScape',
  howlongtobeat: 'HowLongToBeat',
  steam: 'Steam',
  dlsite: 'DLsite',
  getchu: 'Getchu',
  fanza: 'FANZA',
  twitter: 'X',
  pixiv: 'pixiv',
  cien: 'Ci-en',
  youtube: 'YouTube',
  bilibili: 'bilibili',
  wikipedia: '维基百科',
  moegirl: '萌娘百科',
  booth: 'BOOTH',
  official_site: '官网',
  web: '网页',
  curated: '人工',
  user: '人工'
}

interface SiteRule {
  key: string
  host: RegExp
}

const SITE_RULES: SiteRule[] = [
  { key: 'vndb', host: /(^|\.)vndb\.org$/ },
  { key: 'bangumi', host: /(^|\.)(bgm\.tv|bangumi\.tv|chii\.in)$/ },
  { key: 'erogamescape', host: /(^|\.)erogamescape\.(dyndns\.org|org)$/ },
  { key: 'howlongtobeat', host: /(^|\.)howlongtobeat\.com$/ },
  { key: 'steam', host: /(^|\.)(steampowered\.com|steamcommunity\.com)$/ },
  { key: 'cien', host: /^ci-en\.dlsite\.com$/ },
  { key: 'dlsite', host: /(^|\.)dlsite\.com$/ },
  { key: 'getchu', host: /(^|\.)getchu\.com$/ },
  { key: 'fanza', host: /(^|\.)(dmm\.co\.jp|dmm\.com)$/ },
  { key: 'twitter', host: /(^|\.)(twitter\.com|x\.com)$/ },
  { key: 'pixiv', host: /(^|\.)pixiv\.net$/ },
  { key: 'youtube', host: /(^|\.)(youtube\.com|youtu\.be)$/ },
  { key: 'bilibili', host: /(^|\.)(bilibili\.com|b23\.tv)$/ },
  { key: 'wikipedia', host: /(^|\.)wikipedia\.org$/ },
  { key: 'moegirl', host: /(^|\.)moegirl\.org(\.cn)?$/ },
  { key: 'booth', host: /(^|\.)booth\.pm$/ }
]

export const parseHttpUrl = (raw: string) => {
  const text = raw.trim()
  if (!/^https?:\/\//i.test(text)) {
    return null
  }
  try {
    return new URL(text)
  } catch {
    return null
  }
}

export interface LinkSite {
  key: string
  label: string
  host: string
}

export const siteOfUrl = (raw: string): LinkSite | null => {
  const url = parseHttpUrl(raw)
  if (!url) {
    return null
  }
  const host = url.hostname.toLowerCase().replace(/^www\./, '')
  const rule = SITE_RULES.find((entry) => entry.host.test(host))
  return rule
    ? { key: rule.key, label: LINK_SOURCE_LABELS[rule.key] ?? rule.key, host }
    : { key: 'web', label: host, host }
}

const EGS_PAGES = 'https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/'
const DIGITS = /^[0-9]+$/

type PageTemplate = [RegExp, string]

const PERSON_PAGES: Record<string, PageTemplate> = {
  vndb: [/^s[0-9]+$/, 'https://vndb.org/'],
  bangumi: [DIGITS, 'https://bgm.tv/person/'],
  erogamescape: [DIGITS, `${EGS_PAGES}creater.php?creater=`]
}

const IDENTITY_PAGES: Record<string, Record<string, PageTemplate>> = {
  work: {
    vndb: [/^v[0-9]+$/, 'https://vndb.org/'],
    bangumi: [DIGITS, 'https://bgm.tv/subject/'],
    erogamescape: [DIGITS, `${EGS_PAGES}game.php?game=`],
    howlongtobeat: [DIGITS, 'https://howlongtobeat.com/game/'],
    steam: [DIGITS, 'https://store.steampowered.com/app/']
  },
  character: {
    vndb: [/^c[0-9]+$/, 'https://vndb.org/'],
    bangumi: [DIGITS, 'https://bgm.tv/character/'],
    erogamescape: [DIGITS, `${EGS_PAGES}character.php?character=`]
  },
  company: {
    vndb: [/^p[0-9]+$/, 'https://vndb.org/'],
    bangumi: [DIGITS, 'https://bgm.tv/person/'],
    erogamescape: [DIGITS, `${EGS_PAGES}brand.php?brand=`]
  },
  person: PERSON_PAGES,
  credit_name: PERSON_PAGES
}

const BARE_HOST =
  /^[A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+(:[0-9]+)?(\/\S*)?$/
const TWITTER_HANDLE = /^[A-Za-z0-9_]{1,15}$/

const relatedUrl = (source: string, id: string): string | null => {
  const isHttp = /^https?:\/\//i.test(id)
  switch (source) {
    case 'web':
      return isHttp ? id : null
    case 'official_site':
      return isHttp ? id : BARE_HOST.test(id) ? `https://${id}` : null
    case 'twitter':
      return TWITTER_HANDLE.test(id) ? `https://x.com/${id}` : null
    case 'cien':
      return DIGITS.test(id) ? `https://ci-en.dlsite.com/creator/${id}` : null
    case 'steam':
      return DIGITS.test(id)
        ? `https://store.steampowered.com/app/${id}`
        : isHttp
          ? id
          : null
    case 'pixiv':
      return DIGITS.test(id)
        ? `https://www.pixiv.net/users/${id}`
        : isHttp
          ? id
          : null
    case 'dlsite':
      return /^(RJ|VJ|BJ)[0-9]{5,10}$/i.test(id)
        ? `https://www.dlsite.com/home/work/=/product_id/${id.toUpperCase()}.html`
        : null
    default:
      return isHttp ? id : null
  }
}

export const contextLinkUrl = (
  family: string,
  source: string,
  externalId: string
): string | null => {
  const id = externalId.trim()
  if (!source || !id) {
    return null
  }
  const page = IDENTITY_PAGES[family]?.[source]
  if (page && page[0].test(id)) {
    return `${page[1]}${id}`
  }
  return relatedUrl(source, id)
}

export interface ClassifiedLink {
  source: string
  externalId: string
  kind: LinkKind
}

const VNDB_WORK = /^https?:\/\/(?:www\.)?vndb\.org\/(v\d+)(?:[/?#]|$)/i
const BANGUMI_SUBJECT =
  /^https?:\/\/(?:www\.)?(?:bgm\.tv|bangumi\.tv|chii\.in)\/subject\/(\d+)/i
const ARCHIVE = /web\.archive\.org\/web\/[^/]+\/(https?:\/\/.+)$/i
const TWITTER =
  /^https?:\/\/(?:www\.|mobile\.)?(?:twitter\.com|x\.com)\/@?([A-Za-z0-9_]{1,30})(?:[/?#]|$)/i
const CIEN = /^https?:\/\/ci-en\.dlsite\.com\/creator\/(\d+)/i
const STEAM = /^https?:\/\/store\.steampowered\.com\/app\/(\d+)/i
const PIXIV_USERS = /^https?:\/\/(?:www\.)?pixiv\.net\/(?:en\/)?users\/(\d+)/i
const PIXIV_MEMBER = /^https?:\/\/(?:www\.)?pixiv\.net\/member\.php\?id=(\d+)/i
const DLSITE_HOST = /^https?:\/\/(?:www\.)?dlsite\.com\//i
const DLSITE_WORK = /\b((?:RJ|VJ|BJ)\d{5,10})\b/i
const RESERVED_HANDLES = new Set([
  'i',
  'home',
  'intent',
  'share',
  'search',
  'hashtag',
  'explore',
  'messages'
])

export const classifyWorkLink = (raw: string): ClassifiedLink | null => {
  let url = raw.trim()
  const archived = url.match(ARCHIVE)
  if (archived) {
    url = archived[1]!.trim()
  }
  if (!/^https?:\/\//i.test(url)) {
    return null
  }
  const vndb = url.match(VNDB_WORK)
  if (vndb) {
    return {
      source: 'vndb',
      externalId: vndb[1]!.toLowerCase(),
      kind: 'probable'
    }
  }
  const bangumi = url.match(BANGUMI_SUBJECT)
  if (bangumi) {
    return { source: 'bangumi', externalId: bangumi[1]!, kind: 'probable' }
  }
  const related = (source: string, externalId: string): ClassifiedLink => ({
    source,
    externalId,
    kind: 'related'
  })
  const twitter = url.match(TWITTER)
  if (twitter && !RESERVED_HANDLES.has(twitter[1]!.toLowerCase())) {
    return related('twitter', twitter[1]!.toLowerCase())
  }
  const cien = url.match(CIEN)
  if (cien) {
    return related('cien', cien[1]!)
  }
  const steam = url.match(STEAM)
  if (steam) {
    return related('steam', steam[1]!)
  }
  const pixiv = url.match(PIXIV_USERS) ?? url.match(PIXIV_MEMBER)
  if (pixiv) {
    return related('pixiv', pixiv[1]!)
  }
  if (DLSITE_HOST.test(url)) {
    const work = url.match(DLSITE_WORK)
    return work
      ? related('dlsite', work[1]!.toUpperCase())
      : related('web', url)
  }
  return related('web', url)
}

export const linkFingerprint = (source: string, externalId: string) =>
  `${source}:${
    source === 'web' || source === 'official_site'
      ? externalId
          .replace(/^https?:\/\//i, '')
          .replace(/\/+$/, '')
          .toLowerCase()
      : externalId.toLowerCase()
  }`

const SITE_ICONS: Record<string, string> = {
  vndb: 'lucide:database',
  bangumi: 'lucide:database',
  erogamescape: 'lucide:database',
  howlongtobeat: 'lucide:timer',
  steam: 'lucide:shopping-cart',
  dlsite: 'lucide:shopping-cart',
  getchu: 'lucide:shopping-cart',
  fanza: 'lucide:shopping-cart',
  booth: 'lucide:shopping-cart',
  twitter: 'lucide:at-sign',
  pixiv: 'lucide:palette',
  cien: 'lucide:palette',
  youtube: 'lucide:play',
  bilibili: 'lucide:play',
  wikipedia: 'lucide:book-open',
  moegirl: 'lucide:book-open',
  official_site: 'lucide:house',
  internal: 'lucide:anchor'
}

export const siteIconOf = (key: string) => SITE_ICONS[key] ?? 'lucide:globe'
