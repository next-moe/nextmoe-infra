export const WORK_RELATION_PHRASES: Record<
  string,
  { forward: string; reverse: string; symmetric?: boolean }
> = {
  adaptation_of: { forward: '改编自', reverse: '被改编为' },
  sequel_of: { forward: '是…的续作', reverse: '有续作' },
  side_story_of: { forward: '是…的外传', reverse: '有外传' },
  fandisc_of: { forward: '是…的 Fandisc', reverse: '有 Fandisc' },
  collects: { forward: '收录', reverse: '被收录于' },
  remake_of: { forward: '重制自', reverse: '被重制为' },
  same_series: { forward: '同系列', reverse: '同系列', symmetric: true },
  same_setting: { forward: '同世界观', reverse: '同世界观', symmetric: true },
  crossover_with: { forward: '联动', reverse: '联动', symmetric: true },
  shares_character: {
    forward: '角色出演',
    reverse: '角色出演',
    symmetric: true
  },
  alternative_setting: {
    forward: '不同世界观',
    reverse: '不同世界观',
    symmetric: true
  },
  alternative_version: {
    forward: '不同演绎',
    reverse: '不同演绎',
    symmetric: true
  }
}

export const SERIES_MEMBER_KIND_LABELS: Record<string, string> = {
  unknown: '未分类',
  main: '正篇',
  fandisc: 'Fandisc',
  side_story: '外传',
  collection: '合集'
}

export const ROSTER_ROLE_ORDER = ['main', 'secondary', 'appears', 'unknown']

export const VOICE_ACTOR_ROLE_KEY = 'voice-actor'

export const RELEASE_KIND_LABELS: Record<string, string> = {
  default: '正式版',
  digital: '数字版',
  physical: '实体版',
  trial: '体验版',
  patch: '补丁'
}

export const RELEASE_KIND_ORDER = [
  'default',
  'digital',
  'physical',
  'trial',
  'patch'
]
