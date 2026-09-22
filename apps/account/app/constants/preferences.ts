import type { NsfwDisplay } from '~~/shared/types/preferences'

export interface NsfwDisplayOption {
  value: NsfwDisplay
  label: string
  description: string
  adultOnly: boolean
}

export const NSFW_DISPLAY_OPTIONS: NsfwDisplayOption[] = [
  {
    value: 'hide',
    label: '隐藏',
    description: '成人向内容完全不出现在列表与详情里',
    adultOnly: false
  },
  {
    value: 'blur',
    label: '模糊',
    description: '成人向内容以模糊封面出现，点击后才展开',
    adultOnly: true
  },
  {
    value: 'show',
    label: '显示',
    description: '成人向内容与其他内容一样直接展示',
    adultOnly: true
  }
]

export const NSFW_DISPLAY_LABELS: Record<NsfwDisplay, string> =
  Object.fromEntries(
    NSFW_DISPLAY_OPTIONS.map((o) => [o.value, o.label])
  ) as Record<NsfwDisplay, string>

export const PREFERENCE_GLOBAL_NAMESPACE = 'global'

export const ADULT_CONFIRMATION_COPY = {
  title: '确认你已年满 18 岁',
  body: '本站部分作品含成人向内容。确认后你才能把成人向内容的显示方式改为「模糊」或「显示」；确认记录会一直保留，无法撤销。若你未满 18 岁，请直接关闭本窗口。',
  confirm: '我已年满 18 岁',
  cancel: '取消'
}
