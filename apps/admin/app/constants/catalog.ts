export const CATALOG_FILTER_ALL = -1

export const CATALOG_QUEUE_SUMMARY_KEY = 'catalog-queue-summary'

export const CATALOG_ENTITY_TYPES: Record<number, string> = {
  0: '人物',
  1: '名义',
  2: '组织',
  3: '厂牌',
  4: '角色',
  5: '作品',
  6: '版本'
}

export const CATALOG_ENTITY_TYPE = {
  person: 0,
  creditName: 1,
  organization: 2,
  label: 3,
  character: 4,
  work: 5,
  release: 6
} as const

export const CANDIDATE_STATUS = {
  pending: 0,
  accepted: 1,
  rejected: 2,
  deferred: 3,
  needsManual: 4
} as const

export const WORK_STATUS = {
  LIVE: 0,
  STUB: 1,
  MERGED: 2,
  QUARANTINE: 3
} as const

export const WORK_STATUS_LABELS: Record<number, string> = {
  0: '公开',
  1: '未达标',
  2: '已合并',
  3: '隔离中'
}

export const CANDIDATE_STATUS_LABELS: Record<number, string> = {
  0: '待处理',
  1: '已接受',
  2: '已拒绝',
  3: '已搁置',
  4: '待人工'
}

export const CANDIDATE_REASON_LABELS: Record<number, string> = {
  0: '共享外部 ID',
  1: '规范化同名',
  2: '名称近似',
  3: '导入器建议',
  4: 'LLM 建议',
  5: '别名声明'
}

export const PROPOSAL_STATUS = {
  open: 0,
  approved: 1,
  executed: 2,
  rejected: 3,
  withdrawn: 4
} as const

export const PROPOSAL_STATUS_LABELS: Record<number, string> = {
  0: '待审批',
  1: '冷静期',
  2: '已执行',
  3: '已拒绝',
  4: '已撤回'
}

export type CatalogChipColor =
  | 'default'
  | 'primary'
  | 'secondary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'

export const CANDIDATE_STATUS_COLORS: Record<number, CatalogChipColor> = {
  0: 'warning',
  1: 'success',
  2: 'danger',
  3: 'default',
  4: 'secondary'
}

export const PROPOSAL_STATUS_COLORS: Record<number, CatalogChipColor> = {
  0: 'warning',
  1: 'info',
  2: 'success',
  3: 'danger',
  4: 'default'
}

export const MATCHED_BY_KINDS: ReadonlyArray<{
  prefix: string
  label: string
  color: CatalogChipColor
}> = [
  { prefix: 'rule:', label: '规则', color: 'info' },
  { prefix: 'import:', label: '导入', color: 'default' },
  { prefix: 'human:', label: '人工', color: 'success' }
]

export const CATALOG_SOURCE_LABELS: Record<number, string> = {
  1: '人工',
  2: 'VNDB',
  3: 'Bangumi',
  4: 'DLsite',
  5: 'ErogameScape',
  6: 'AniList',
  7: 'MAL',
  8: 'Steam',
  9: '官网',
  10: 'Twitter',
  11: 'Pixiv',
  12: '策展',
  13: '超分',
  14: 'Ci-en',
  15: 'DMM',
  16: '网页',
  17: 'Getchu',
  18: '派生',
  19: 'NextMoe',
  20: 'HowLongToBeat'
}

export const CATALOG_SOURCE = {
  user: 1,
  vndb: 2,
  bangumi: 3,
  dlsite: 4,
  erogamescape: 5,
  anilist: 6,
  mal: 7,
  steam: 8,
  officialSite: 9,
  twitter: 10,
  pixiv: 11,
  curated: 12,
  upscale: 13,
  cien: 14,
  dmm: 15,
  web: 16,
  getchu: 17,
  derived: 18,
  nextmoe: 19,
  howlongtobeat: 20
} as const

export const CATALOG_LINK_KIND = {
  exact: 0,
  probable: 1,
  related: 2
} as const

export const CATALOG_MEDIUM_LABELS: Record<number, string> = {
  1: 'Galgame',
  2: '漫画',
  3: '小说',
  4: '动画',
  5: 'ASMR'
}

export const CATALOG_MEDIUM_COLORS: Record<number, CatalogChipColor> = {
  1: 'primary',
  2: 'warning',
  3: 'info',
  4: 'secondary',
  5: 'default'
}

export const CONTENT_RATING_LABELS: Record<number, string> = {
  0: '全年龄',
  1: '敏感',
  2: 'R18'
}

export const CONTENT_RATING_COLORS: Record<number, CatalogChipColor> = {
  0: 'success',
  1: 'warning',
  2: 'danger'
}

export const CLAIM_STATE_LABELS: Record<number, string> = {
  0: '已上线',
  1: '草稿',
  2: '隐藏',
  3: '待审核',
  4: '已拒绝'
}

const startsWithLetter = (externalId: string) => /^[A-Za-z]/.test(externalId)

export const catalogExternalUrl = (
  sourceId: number,
  externalId: string,
  entityType?: number
): string | null => {
  const id = externalId.trim()
  if (!id) return null
  switch (sourceId) {
    case CATALOG_SOURCE.vndb: {
      if (startsWithLetter(id)) return `https://vndb.org/${id}`
      const prefix = entityType === CATALOG_ENTITY_TYPE.release ? 'r' : 'v'
      return `https://vndb.org/${prefix}${id}`
    }
    case CATALOG_SOURCE.bangumi:
      return `https://bangumi.tv/subject/${id}`
    case CATALOG_SOURCE.steam:
      return `https://store.steampowered.com/app/${id}`
    case CATALOG_SOURCE.howlongtobeat:
      return `https://howlongtobeat.com/game/${id}`
    default:
      return null
  }
}

export const CATALOG_IMAGE_REF_KIND_LABELS: Record<string, string> = {
  work_cover: '封面',
  work_screenshot: '截图',
  character_bust: '角色胸像',
  character_figure: '角色立绘',
  label_logo: '会社 logo',
  person_photo: '人物照片'
}
