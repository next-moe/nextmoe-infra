import type {
  WtEndingType,
  WtLang,
  WtNodeType,
  WtStepType
} from '#shared/types/walkthrough'

export const WT_FIELD = {
  chart: 'catalog.walkthrough.chart',
  hidden: 'catalog.walkthrough.hidden',
  lang: 'catalog.walkthrough.lang',
  pins: 'catalog.walkthrough.pins',
  work: 'catalog.walkthrough.work_id',
  release: 'catalog.walkthrough.work_release_id'
} as const

export const WT_KEY_PATTERN = /^[A-Za-z0-9_]{1,32}$/

export const WT_LIMITS = {
  nameRunes: 512,
  labelRunes: 256,
  textRunes: 1024,
  conditionRunes: 1024,
  aliases: 32,
  alts: 16,
  characters: 128,
  segments: 2000,
  steps: 6000
}

export const WT_ENDING_TYPES: { value: WtEndingType; label: string }[] = [
  { value: 'heroine', label: '个人线' },
  { value: 'harem', label: '后宫' },
  { value: 'true', label: '真结局' },
  { value: 'normal', label: '普通结局' },
  { value: 'bad', label: '坏结局' },
  { value: 'after', label: '后日谈' },
  { value: 'other', label: '其他' }
]

export const WT_ENDING_LABEL = Object.fromEntries(
  WT_ENDING_TYPES.map((entry) => [entry.value, entry.label])
) as Record<WtEndingType, string>

export const WT_STEP_META: Record<
  WtStepType,
  { label: string; icon: string; placeholder: string }
> = {
  choice: {
    label: '选项',
    icon: 'lucide:mouse-pointer-click',
    placeholder: '游戏内选项的原文'
  },
  action: {
    label: '操作',
    icon: 'lucide:footprints',
    placeholder: '需要做的事，例如「进入图书馆」'
  },
  save: { label: '存档', icon: 'lucide:save', placeholder: '' },
  heading: {
    label: '小标题',
    icon: 'lucide:heading',
    placeholder: '给下面几步分组的标题，例如「5月14日」「第一章」'
  },
  note: {
    label: '说明',
    icon: 'lucide:info',
    placeholder: '写给玩家的提示，例如「※看过沙耶END时选“是”」'
  }
}

export const WT_START_NODES: { value: WtNodeType; label: string }[] = [
  { value: 'entry', label: '入口' },
  { value: 'save', label: '存档' },
  { value: 'ending', label: '结局之后' }
]

export const WT_END_NODES: { value: WtNodeType; label: string }[] = [
  { value: 'ending', label: '结局' },
  { value: 'collect', label: '回收后结束' },
  { value: 'open', label: '未完待续' }
]

export const WT_NODE_LABEL: Record<WtNodeType, string> = {
  entry: '入口',
  save: '存档',
  ending: '结局',
  collect: '回收后结束',
  open: '未完待续'
}

export const WT_LANGS: { value: WtLang; label: string }[] = [
  { value: 'ja', label: '日文原文' },
  { value: 'zh-Hans', label: '简体中文' }
]

export const WT_TONES = [
  {
    chip: 'bg-primary-100 text-primary-700',
    dot: 'bg-primary-500',
    stroke: 'stroke-primary-400',
    fill: 'fill-primary-400',
    border: 'border-primary-400',
    bar: 'bg-primary-400'
  },
  {
    chip: 'bg-secondary-100 text-secondary-700',
    dot: 'bg-secondary-500',
    stroke: 'stroke-secondary-400',
    fill: 'fill-secondary-400',
    border: 'border-secondary-400',
    bar: 'bg-secondary-400'
  },
  {
    chip: 'bg-success-100 text-success-700',
    dot: 'bg-success-500',
    stroke: 'stroke-success-500',
    fill: 'fill-success-500',
    border: 'border-success-500',
    bar: 'bg-success-500'
  },
  {
    chip: 'bg-warning-100 text-warning-700',
    dot: 'bg-warning-500',
    stroke: 'stroke-warning-500',
    fill: 'fill-warning-500',
    border: 'border-warning-500',
    bar: 'bg-warning-500'
  },
  {
    chip: 'bg-danger-100 text-danger-700',
    dot: 'bg-danger-500',
    stroke: 'stroke-danger-400',
    fill: 'fill-danger-400',
    border: 'border-danger-400',
    bar: 'bg-danger-400'
  },
  {
    chip: 'bg-info-100 text-info-700',
    dot: 'bg-info-500',
    stroke: 'stroke-info-500',
    fill: 'fill-info-500',
    border: 'border-info-500',
    bar: 'bg-info-500'
  }
] as const

export const WT_NEUTRAL_TONE = {
  chip: 'bg-default-100 text-default-700',
  dot: 'bg-default-400',
  stroke: 'stroke-default-300',
  fill: 'fill-default-300',
  border: 'border-default-300',
  bar: 'bg-default-300'
} as const

export type WtTone = (typeof WT_TONES)[number] | typeof WT_NEUTRAL_TONE

export const wtTone = (index: number | null): WtTone =>
  index === null ? WT_NEUTRAL_TONE : WT_TONES[index % WT_TONES.length]!

export const WT_NODE_SIZE: Record<
  WtNodeType,
  { width: number; height: number }
> = {
  entry: { width: 120, height: 40 },
  save: { width: 112, height: 36 },
  ending: { width: 184, height: 48 },
  collect: { width: 96, height: 30 },
  open: { width: 64, height: 28 }
}

export const WT_SHORTCUTS: { keys: string; label: string }[] = [
  { keys: 'Enter', label: '在下方新建一步' },
  { keys: 'Backspace', label: '在空步骤上删除这一步' },
  { keys: 'ArrowUp', label: '移到上一个输入框' },
  { keys: 'ArrowDown', label: '移到下一个输入框' },
  { keys: 'Alt+ArrowUp', label: '把这一步上移' },
  { keys: 'Alt+ArrowDown', label: '把这一步下移' },
  { keys: 'Mod+D', label: '复制这一步' },
  { keys: 'Mod+Backspace', label: '删除这一步' },
  { keys: 'Alt+A', label: '切换 ☆ 标记' },
  { keys: 'Mod+Z', label: '撤销' },
  { keys: 'Mod+Shift+Z', label: '重做' },
  { keys: 'Mod+S', label: '提交修改' },
  { keys: 'Escape', label: '离开输入框 / 取消选择' },
  { keys: '?', label: '显示这份快捷键' }
]
