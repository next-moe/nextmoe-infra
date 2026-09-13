import type { KunSiteConfig } from './config'

const KUN_SITE_NAME = 'NextMoe·未萌 账号'
const KUN_SITE_SHORT = 'NextMoe·未萌 账号'
const KUN_SITE_MENTION = '@nextmoe'
const KUN_SITE_TITLE = 'NextMoe·未萌 账号'
const KUN_SITE_DESCRIPTION = '统一 NextMoe 站点集群用户与服务的账户中心'
const KUN_SITE_URL = 'https://account.nextmoe.com'

const KUN_SITE_BRAND = 'https://www.nextmoe.com'
const KUN_SITE_FORUM = 'https://www.kungal.com'
const KUN_SITE_NAV = 'https://nav.kungal.org'
const KUN_SITE_PATCH = 'https://www.moyu.moe'
const KUN_SITE_STICKER = 'https://sticker.kungal.com'
const KUN_SITE_OSS_DOMAIN = 'https://kun-galgame-forum.iloveren.link'
const KUN_SITE_DEVELOPMENT_DOCUMENTATION = 'https://docs-kungal.nextmoe.dev'
const KUN_SITE_TELEGRAM_GROUP = 'https://t.me/kungalgame'
const KUN_SITE_GITHUB = 'https://github.com/next-moe'
const KUN_SITE_AUTHOR_GITHUB = 'https://github.com/next-moe'
const KUN_SITE_LIST = [
  { name: 'NextMoe 主站', url: KUN_SITE_BRAND },
  { name: 'Galgame 导航', url: KUN_SITE_NAV },
  { name: 'Galgame 补丁', url: KUN_SITE_PATCH },
  { name: 'Galgame 表情包', url: KUN_SITE_STICKER },
  { name: 'Galgame 论坛', url: KUN_SITE_FORUM },
  { name: 'NextMoe 开发者平台', url: KUN_SITE_DEVELOPMENT_DOCUMENTATION }
]
const KUN_SITE_THEME_COLOR = '#006FEE'
const KUN_SITE_VALID_DOMAIN_LIST = ['account.nextmoe.com']

const KUN_SITE_KEYWORDS = [
  'NextMoe',
  '未萌',
  'NextMoe 账号',
  'ACGN',
  'Galgame'
]

export const account: KunSiteConfig = {
  name: KUN_SITE_NAME,
  title: KUN_SITE_TITLE,
  titleShort: KUN_SITE_SHORT,
  titleTemplate: `%s - ${KUN_SITE_TITLE}`,
  description: KUN_SITE_DESCRIPTION,
  keywords: KUN_SITE_KEYWORDS,
  canonical: KUN_SITE_URL,
  themeColor: KUN_SITE_THEME_COLOR,
  github: KUN_SITE_GITHUB,
  authorGitHub: KUN_SITE_AUTHOR_GITHUB,
  validDomain: KUN_SITE_VALID_DOMAIN_LIST,
  author: [
    { name: KUN_SITE_TITLE, url: KUN_SITE_URL },
    { name: 'GitHub', url: KUN_SITE_GITHUB },
    ...KUN_SITE_LIST
  ],
  creator: {
    name: KUN_SITE_SHORT,
    mention: KUN_SITE_MENTION,
    url: KUN_SITE_URL
  },
  publisher: {
    name: KUN_SITE_SHORT,
    mention: KUN_SITE_MENTION,
    url: KUN_SITE_URL
  },
  domain: {
    main: KUN_SITE_URL,
    forum: KUN_SITE_FORUM,
    imageBed: 'https://img.kungal.com',
    storage: 'https://oss.kungal.com',
    telegram_group: KUN_SITE_TELEGRAM_GROUP,
    patch: KUN_SITE_PATCH,
    backup: KUN_SITE_FORUM,
    sticker: KUN_SITE_STICKER,
    nav: KUN_SITE_NAV,
    doc: KUN_SITE_DEVELOPMENT_DOCUMENTATION,
    oss: KUN_SITE_OSS_DOMAIN
  },
  og: {
    title: KUN_SITE_TITLE,
    description: KUN_SITE_DESCRIPTION,
    image: '/favicon.webp',
    url: KUN_SITE_URL
  },
  ad: [],
  images: [
    {
      url: '/favicon.webp',
      fullUrl: `${KUN_SITE_URL}/favicon.webp`,
      width: 256,
      height: 256,
      alt: KUN_SITE_TITLE
    }
  ]
}
