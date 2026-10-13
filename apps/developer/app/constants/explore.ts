// v2 is default-thin: every block below the identity core has to be asked for
// by name, and an unknown token is a 400 rather than a silently missing block.
export const EXPLORE_WORK_INCLUDE = [
  'titles',
  'refs',
  'intros',
  'covers',
  'screenshots',
  'tags',
  'characters',
  'companies',
  'series',
  'releases',
  'ratings',
  'playtimes',
  'popularity',
  'relations',
  'credits',
  'engines',
  'links'
].join(',')

export const EXPLORE_RATING_SCALE: Record<string, number> = {
  nextmoe: 10,
  vndb: 10,
  bangumi: 10,
  dlsite: 5,
  erogamescape: 100,
  howlongtobeat: 100
}

export const EXPLORE_RATING_ORDER = [
  'nextmoe',
  'vndb',
  'bangumi',
  'erogamescape',
  'dlsite',
  'howlongtobeat'
]

export const EXPLORE_POPULARITY_LABEL: Record<string, string> = {
  favorites: '收藏',
  downloads: '下载',
  wishlist: '愿望单',
  reviews: '评论',
  bgm_wish: '想玩',
  bgm_collect: '玩过',
  bgm_doing: '在玩',
  bgm_on_hold: '搁置',
  bgm_dropped: '抛弃'
}

export const EXPLORE_POPULARITY_TONE: Record<string, string> = {
  bgm_collect: 'bg-success',
  bgm_doing: 'bg-primary',
  bgm_wish: 'bg-secondary',
  bgm_on_hold: 'bg-warning',
  bgm_dropped: 'bg-danger'
}

export const EXPLORE_MEDIUM_ICON: Record<string, string> = {
  galgame: 'lucide:gamepad-2',
  doujin_game: 'lucide:joystick',
  anime: 'lucide:tv',
  manga: 'lucide:book-open',
  novel: 'lucide:book',
  asmr: 'lucide:headphones',
  music: 'lucide:music'
}

export const EXPLORE_COVER_KIND_LABEL: Record<string, string> = {
  main: '主视觉',
  dig: '数字版',
  pkgfront: '包装正面',
  pkgback: '包装背面',
  pkgside: '包装侧面',
  pkgmed: '光盘',
  pkgcontent: '包装内容'
}

export const EXPLORE_ROSTER_SECTIONS = [
  { role: 'main', label: '主要角色' },
  { role: 'secondary', label: '次要角色' },
  { role: 'appears', label: '登场角色' },
  { role: 'unknown', label: '其他角色' }
] as const

export const EXPLORE_STAFF_LEAD_ROLES = [
  '原作',
  '导演',
  '制作人',
  '脚本',
  '剧本',
  '原画',
  '人物设定',
  'SD原画',
  '插画',
  '美工',
  '背景',
  '音乐',
  '作曲',
  '编曲',
  '作词',
  '主题歌演出',
  '主题歌作曲',
  '主题歌作词',
  '插入歌演出',
  '声乐',
  '动画制作',
  '程序',
  '音响',
  '开发',
  '发行'
]

export const EXPLORE_STAFF_TAIL_ROLES = [
  '翻译',
  '编辑',
  'QA',
  '协力',
  '特别鸣谢',
  '其他'
]

export const EXPLORE_SOURCE_URL: Record<string, (id: string) => string> = {
  vndb: (id) => `https://vndb.org/${id}`,
  bangumi: (id) => `https://bgm.tv/subject/${id}`,
  steam: (id) => `https://store.steampowered.com/app/${id}`,
  dlsite: (id) => `https://www.dlsite.com/home/work/=/product_id/${id}.html`,
  erogamescape: (id) =>
    `https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/game.php?game=${id}`,
  getchu: (id) => `https://www.getchu.com/soft.phtml?id=${id}`,
  howlongtobeat: (id) => `https://howlongtobeat.com/game/${id}`
}
