export const CONTRIBUTION_FAMILIES = [
  { key: 'work', label: '作品' },
  { key: 'release', label: '发行' },
  { key: 'character', label: '角色' },
  { key: 'person', label: '人物' },
  { key: 'credit_name', label: '署名名' },
  { key: 'company', label: '厂商' },
  { key: 'tag', label: '标签' },
  { key: 'series', label: '系列' },
  { key: 'engine', label: '引擎' }
] as const

export const CONTRIBUTION_STATES = [
  'open',
  'merged',
  'declined',
  'withdrawn'
] as const

export const ACTIVITY_WEEKS = 12
