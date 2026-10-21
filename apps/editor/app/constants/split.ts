export const SPLIT_GROUP_OPTIONS = [
  { value: 'kind', label: '按版本类型' },
  { value: 'platform', label: '按平台' },
  { value: 'lang', label: '按语言' },
  { value: 'none', label: '不分组' }
] as const

export const SPLIT_MOVED_LABELS: Record<string, string> = {
  releases: '移走的发行',
  tombstones: '留在原作品的「已移走」记录',
  titles: '新作品的标题',
  credits: '只限这些发行的署名',
  refs: '设为新作品正式对应的条目',
  rejections: '防止配回原作品的记录'
}

export const SPLIT_COUNT_LABELS: Record<string, string> = {
  releases: '发行',
  release_labels: '发行上的厂商',
  release_refs: '发行在各资料站的对应',
  credits: '署名',
  tombstones: '「已移走」记录',
  characters: '角色',
  tags: '标签',
  titles: '标题'
}

export const SPLIT_SOURCE_LABELS: Record<string, string> = {
  bangumi: 'Bangumi',
  erogamescape: '批评空间'
}

export const SPLIT_SOURCE_URLS: Record<string, (id: string) => string> = {
  bangumi: (id) => `https://bgm.tv/subject/${id}`,
  erogamescape: (id) =>
    `https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/game.php?game=${id}`
}

export const SPLIT_TITLE_KIND_OPTIONS = [
  { value: 'official', label: '正式名' },
  { value: 'alias', label: '别名' },
  { value: 'abbreviation', label: '简称' }
] as const

export const SPLIT_REASON_HINTS = [
  '这个版本追加了新的路线或大量新内容，玩法和剧情体验不同于原版',
  'Bangumi 和批评空间都把它单独收录成一个条目',
  '这是独立发售、独立定价的作品，不是补丁或移植'
]
