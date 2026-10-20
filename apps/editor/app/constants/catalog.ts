import type { KunUIColor } from '@kungal/ui-core'

export const CONTENT_RATING: Record<
  string,
  { label: string; color: KunUIColor }
> = {
  all_ages: { label: '全年龄', color: 'success' },
  sensitive: { label: '敏感', color: 'warning' },
  r18: { label: 'R18', color: 'danger' }
}

export const MEDIUM_LABELS: Record<string, string> = {
  galgame: 'Galgame',
  manga: '漫画',
  novel: '小说',
  anime: '动画',
  asmr: 'ASMR',
  doujin_game: '同人游戏',
  music: '音乐'
}

export const RELEASE_STATUS_LABELS: Record<string, string> = {
  released: '已发售',
  dated: '已定档',
  announced: '已公布',
  cancelled: '已取消',
  unknown: '未知'
}

export const TITLE_KIND_LABELS: Record<string, string> = {
  official: '正式名',
  alias: '别名',
  abbreviation: '简称'
}

export const ATTRIBUTION_ROLE_LABELS: Record<string, string> = {
  developer: '开发',
  publisher: '发行',
  brand: '品牌',
  circle: '社团'
}

export const ROSTER_ROLE_LABELS: Record<string, string> = {
  main: '主要角色',
  secondary: '次要角色',
  appears: '登场',
  unknown: '未分类'
}

export const SPOILER_LABELS: Record<string, string> = {
  none: '',
  minor: '轻微剧透',
  major: '严重剧透'
}

export const PROPOSAL_STATE: Record<
  string,
  { label: string; color: KunUIColor }
> = {
  open: { label: '待审', color: 'warning' },
  merged: { label: '已合并', color: 'success' },
  declined: { label: '已驳回', color: 'danger' },
  withdrawn: { label: '已撤回', color: 'default' }
}

export const REVISION_ACTION: Record<
  string,
  { label: string; tone: string; icon: string }
> = {
  created: {
    label: '创建',
    tone: 'bg-primary-50 text-primary-600',
    icon: 'lucide:plus'
  },
  merged: {
    label: '审核合并',
    tone: 'bg-success-50 text-success-600',
    icon: 'lucide:git-merge'
  },
  direct: {
    label: '直接编辑',
    tone: 'bg-info-50 text-info-600',
    icon: 'lucide:pencil'
  },
  reverted: {
    label: '回滚',
    tone: 'bg-warning-50 text-warning-600',
    icon: 'lucide:undo-2'
  }
}

export const ENTITY_FAMILY: Record<string, { label: string; path: string }> = {
  work: { label: '作品', path: '/works' },
  company: { label: '厂商', path: '/companies' },
  character: { label: '角色', path: '/characters' },
  release: { label: '发行', path: '/releases' },
  tag: { label: '标签', path: '/tags' },
  engine: { label: '引擎', path: '/engines' },
  series: { label: '系列', path: '/series' },
  person: { label: '人物', path: '/persons' },
  credit_name: { label: '署名名', path: '/credit-names' },
  trait: { label: '特质', path: '/traits' }
}

export const LANGUAGE_LABELS: Record<string, string> = {
  ja: '日语',
  'zh-Hans': '简体中文',
  'zh-Hant': '繁体中文',
  zh: '中文',
  en: '英语',
  ko: '韩语'
}

export const SITE_LABELS: Record<string, string> = {
  kungal: '鲲 Galgame',
  letmoe: 'LetMoe',
  editor: '未萌编辑台',
  nextmoe: 'NextMoe',
  galgame_wiki: '旧 Galgame Wiki'
}

export const WORK_DETAIL_INCLUDE = [
  'titles',
  'intros',
  'tags',
  'companies',
  'engines',
  'series',
  'relations',
  'covers',
  'screenshots',
  'links',
  'platforms',
  'credits',
  'characters'
].join(',')
