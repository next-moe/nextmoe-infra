import type { NsfwDisplay } from '~~/shared/types/preferences'

export interface NsfwDisplayOption {
  value: NsfwDisplay
  label: string
  description: string
}

export const NSFW_DISPLAY_OPTIONS: NsfwDisplayOption[] = [
  {
    value: 'hide',
    label: '隐藏',
    description: '成人向内容完全不出现在列表与详情里'
  },
  {
    value: 'blur',
    label: '模糊',
    description: '成人向内容以模糊封面出现，点击后才展开'
  },
  {
    value: 'show',
    label: '显示',
    description: '成人向内容与其他内容一样直接展示'
  }
]

export const NSFW_DISPLAY_LABELS: Record<NsfwDisplay, string> =
  Object.fromEntries(
    NSFW_DISPLAY_OPTIONS.map((o) => [o.value, o.label])
  ) as Record<NsfwDisplay, string>

export const PREFERENCE_GLOBAL_NAMESPACE = 'global'
