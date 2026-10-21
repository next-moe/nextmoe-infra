import type { GuidelineCategory } from '#shared/types/guidelines'

export interface GuidelineCategoryMeta {
  key: GuidelineCategory
  label: string
  description: string
  icon: string
}

export const GUIDELINE_CATEGORIES: GuidelineCategoryMeta[] = [
  {
    key: 'start',
    label: '入门',
    description: '第一次编辑先读这里：编辑台怎么用、资料从哪里来。',
    icon: 'lucide:rocket'
  },
  {
    key: 'rules',
    label: '规则与流程',
    description: '谁能改什么、每天能改多少、提案怎样被审核和回滚。',
    icon: 'lucide:scale'
  },
  {
    key: 'work',
    label: '作品',
    description: '作品条目里每一个分区的填写方法。',
    icon: 'lucide:gamepad-2'
  },
  {
    key: 'people',
    label: '人物与角色',
    description: '署名名、人物、制作人员、配音与角色。',
    icon: 'lucide:users'
  },
  {
    key: 'vocab',
    label: '厂商与词表',
    description: '厂商条目，以及标签、特质这两套词表。',
    icon: 'lucide:tags'
  },
  {
    key: 'appendix',
    label: '附录',
    description: '术语表与常见问题。',
    icon: 'lucide:book-marked'
  }
]

export interface GuidelinePrinciple {
  title: string
  body: string
  icon: string
}

export const GUIDELINE_PRINCIPLES: GuidelinePrinciple[] = [
  {
    title: '有出处才改',
    body: '以官方资料为准，其次是可信的资料站。修改说明里写清出处，审核会快很多。',
    icon: 'lucide:book-check'
  },
  {
    title: '照原样记录',
    body: '名字、标题按官方写法原样填写，不翻译、不美化；译名单独作为别名。',
    icon: 'lucide:quote'
  },
  {
    title: '拿不准就往严重处标',
    body: '剧透等级、图片分级、内容分级，犹豫时选更高的一档。',
    icon: 'lucide:shield-alert'
  },
  {
    title: '隐藏而不是删除',
    body: '资料源导入的内容只能隐藏，导入任务不会把你的修改改回去。',
    icon: 'lucide:eye-off'
  },
  {
    title: '一次只做一件事',
    body: '一份提案只改一类问题，审核者才能逐项判断；无关的修改分开提交。',
    icon: 'lucide:list-checks'
  }
]

export interface GuidelinePitfall {
  title: string
  body: string
  to: string
}

export const GUIDELINE_PITFALLS: GuidelinePitfall[] = [
  {
    title: '把署名名当成人物',
    body: '署名名是职员表上印的名字，人物是背后的那个人。一个人的不同笔名挂在同一个人物下；同名的不同人不要合并。',
    to: '/guidelines/credit-names-and-persons'
  },
  {
    title: '以为“删除”能去掉资料源的内容',
    body: '资料源导入的行只能隐藏。隐藏后导入任务不会再把它加回来，随时可以恢复。',
    to: '/guidelines/upstream-data'
  },
  {
    title: '直接改显示名',
    body: '显示名会按原语言的正式标题自动重算。想改名，通常应该改标题。',
    to: '/guidelines/titles'
  },
  {
    title: '把机翻或汉化组译名标成正式名',
    body: '只有官方发布过的名字才是正式名；其他译名作为别名。',
    to: '/guidelines/titles'
  },
  {
    title: '剧透和图片分级标得太低',
    body: '剧透等级、性暗示与暴力分级拿不准时往高处标，避免未准备的读者被揭底或看到露骨内容。',
    to: '/guidelines/media'
  },
  {
    title: '想隐藏身份链接',
    body: '身份链接决定条目对应哪个外部条目，不能隐藏。认错了条目要发起合并请求或联系审核者。',
    to: '/guidelines/links'
  },
  {
    title: '新建了一个重复条目',
    body: '新建前先搜索。已经重复的条目不要各自修改，发起合并请求。',
    to: '/guidelines/create-and-merge'
  },
  {
    title: '提交后看不到修改',
    body: '需要审核的提案在通过前不会显示。可以在「我的提案」里查看状态。',
    to: '/guidelines/proposals-and-review'
  }
]
