import type { KunUIColor } from '@kungal/ui-core'

export type MergeSectionKey =
  | 'overview'
  | 'fields'
  | 'titles'
  | 'intros'
  | 'releases'
  | 'anchors'
  | 'related'
  | 'users'
  | 'claim'

export interface MergeSectionDef {
  key: MergeSectionKey
  label: string
  icon: string
  description: string
  worksOnly?: boolean
}

export const MERGE_SECTIONS: MergeSectionDef[] = [
  {
    key: 'overview',
    label: '概览',
    icon: 'lucide:layout-dashboard',
    description: '合并的方向、依据，以及需要先看的问题。'
  },
  {
    key: 'fields',
    label: '基本信息',
    icon: 'lucide:settings-2',
    description: '每一项选择保留哪一边的值。'
  },
  {
    key: 'titles',
    label: '标题',
    icon: 'lucide:heading',
    description: '保留方已有标题的语言里，被并入方的标题改为别名。',
    worksOnly: true
  },
  {
    key: 'intros',
    label: '简介',
    icon: 'lucide:align-left',
    description: '每种语言只保留一份简介，保留方自己的原文优先。',
    worksOnly: true
  },
  {
    key: 'releases',
    label: '发行',
    icon: 'lucide:package',
    description:
      '被并入方的发行全部移到保留方；与保留方重复的可以在合并后隐藏。',
    worksOnly: true
  },
  {
    key: 'anchors',
    label: '资料源对应',
    icon: 'lucide:link',
    description: '每个外部条目合并后的去向。'
  },
  {
    key: 'related',
    label: '关联数据',
    icon: 'lucide:git-fork',
    description: '标签、制作人员、角色等会一起移到保留方，两边都有的合为一条。'
  },
  {
    key: 'users',
    label: '用户数据',
    icon: 'lucide:users',
    description: '评分、收藏、游玩记录跟随作品移到保留方。',
    worksOnly: true
  },
  {
    key: 'claim',
    label: '认领与下游',
    icon: 'lucide:radio-tower',
    description: '站点认领的去留，以及下游站点如何跟随。',
    worksOnly: true
  }
]

export const MERGE_SIDE_LABELS = {
  source: '被并入',
  target: '保留',
  result: '合并后'
} as const

export const MERGE_FATE: Record<
  string,
  { label: string; color: KunUIColor; hint: string }
> = {
  stays: { label: '保留', color: 'default', hint: '保留方原有，不变' },
  moves: { label: '移入', color: 'success', hint: '原样移到保留方' },
  becomes_alias: {
    label: '改为别名',
    color: 'primary',
    hint: '保留方在这个语言已有标题，它改为不分语言的别名'
  },
  duplicate: {
    label: '重复',
    color: 'default',
    hint: '保留方已有相同的一条，合为一条'
  },
  dropped: {
    label: '丢弃',
    color: 'warning',
    hint: '机器翻译的标题，保留方已有这个语言，不再保留'
  },
  kept_out: {
    label: '不采用',
    color: 'default',
    hint: '保留方自己的原文优先，这份存档备查'
  },
  replaces_machine: {
    label: '替换机翻',
    color: 'success',
    hint: '保留方只有机器翻译，改用这份原文'
  },
  demoted_related: {
    label: '降为相关',
    color: 'warning',
    hint: '保留方在这个资料源已有正式对应，它改为相关链接'
  },
  demoted_probable: {
    label: '降为待确认',
    color: 'warning',
    hint: '保留方在这个资料源已有正式对应，它改为待确认'
  }
}

export const MERGE_LINK_KIND_LABELS: Record<string, string> = {
  identity: '正式对应',
  probable: '待确认',
  related: '相关链接'
}

export const MERGE_COUNT_LABELS: Record<string, string> = {
  tags: '标签',
  credits: '制作人员',
  characters: '角色',
  companies: '厂商',
  relations: '关联作品',
  series: '系列',
  covers: '封面',
  screenshots: '截图',
  platforms: '平台',
  user_ratings: '评分',
  user_folders: '收藏夹条目',
  user_states: '游玩状态',
  user_playtime: '游玩时长'
}

export const MERGE_USER_COUNT_KEYS = [
  'user_ratings',
  'user_folders',
  'user_states',
  'user_playtime'
]

export const MERGE_TABLE_LABELS: Record<string, string> = {
  catalog_release: '发行',
  catalog_work_title: '标题',
  catalog_work_intro: '简介',
  catalog_work_tag: '标签',
  catalog_credit: '制作人员',
  catalog_work_character: '角色',
  catalog_work_label: '厂商',
  catalog_work_relation: '关联作品',
  catalog_series_member: '系列',
  catalog_work_cover: '封面',
  catalog_work_screenshot: '截图',
  catalog_work_platform: '平台',
  catalog_work_engine: '引擎',
  catalog_external_ref: '资料源对应',
  catalog_user_work_rating: '评分',
  catalog_user_folder_item: '收藏夹条目',
  catalog_user_work_state: '游玩状态',
  catalog_user_playtime: '游玩时长',
  catalog_redirect: '旧重定向',
  edit_suppressed_row: '隐藏记录'
}

export const MERGE_FIELD_LABELS: Record<string, string> = {
  display_name: '显示名',
  olang: '原语言',
  latin: '罗马字',
  lang: '语言',
  gender: '性别',
  image_hash: '图片',
  note: '备注',
  logo_hash: 'Logo',
  birth: '生日',
  primary_credit_name_id: '主署名名'
}
