import type { EditGroup } from '#shared/types/edit'
import type { EditFamily } from './editEntities'

export type WorkspaceSectionGroup = 'profile' | 'media' | 'related' | 'other'

export const WORKSPACE_GROUP_LABELS: Record<WorkspaceSectionGroup, string> = {
  profile: '资料',
  media: '媒体',
  related: '关联条目',
  other: '其他'
}

export const WORKSPACE_GROUP_ORDER: WorkspaceSectionGroup[] = [
  'profile',
  'media',
  'related',
  'other'
]

export interface WorkspaceSectionDef {
  key: string
  label: string
  icon: string
  group: WorkspaceSectionGroup
  fields: string[]
  view?: string
  always?: boolean
  guide?: string
}

const work = (key: string) => `catalog.work.${key}`

export const WORK_SECTIONS: WorkspaceSectionDef[] = [
  {
    key: 'basic',
    guide: 'work-basics',
    label: '基础',
    icon: 'lucide:settings-2',
    group: 'profile',
    view: 'work:basic',
    fields: ['display_name', 'olang', 'content_rating', 'display_nsfw'].map(
      work
    )
  },
  {
    key: 'titles',
    guide: 'titles',
    label: '标题',
    icon: 'lucide:heading',
    group: 'profile',
    view: 'work:titles',
    fields: [work('titles')]
  },
  {
    key: 'intros',
    guide: 'intros',
    label: '简介',
    icon: 'lucide:align-left',
    group: 'profile',
    view: 'work:intros',
    fields: [work('intros')]
  },
  {
    key: 'tags',
    guide: 'tags-and-spoilers',
    label: '标签',
    icon: 'lucide:tags',
    group: 'profile',
    view: 'work:tags',
    fields: [work('tag_ids'), work('tag_spoilers')]
  },
  {
    key: 'links',
    guide: 'links',
    label: '外链',
    icon: 'lucide:link',
    group: 'profile',
    view: 'work:links',
    fields: [work('links')]
  },
  {
    key: 'covers',
    guide: 'media',
    label: '封面',
    icon: 'lucide:book-image',
    group: 'media',
    view: 'work:covers',
    fields: [work('covers'), work('covers_upstream'), work('cover_order')]
  },
  {
    key: 'screenshots',
    guide: 'media',
    label: '截图',
    icon: 'lucide:images',
    group: 'media',
    view: 'work:screenshots',
    fields: [
      work('screenshots'),
      work('screenshots_upstream'),
      work('screenshot_order')
    ]
  },
  {
    key: 'roster',
    guide: 'characters',
    label: '角色',
    icon: 'lucide:users',
    group: 'related',
    view: 'work:roster',
    fields: [work('roster')]
  },
  {
    key: 'credits',
    guide: 'staff-credits',
    label: '制作人员',
    icon: 'lucide:clapperboard',
    group: 'related',
    view: 'work:credits',
    fields: [work('credits'), work('credits_upstream')]
  },
  {
    key: 'companies',
    guide: 'companies-engines-platforms',
    label: '厂商',
    icon: 'lucide:building-2',
    group: 'related',
    view: 'work:companies',
    fields: [work('labels')]
  },
  {
    key: 'engines',
    guide: 'companies-engines-platforms',
    label: '引擎与平台',
    icon: 'lucide:cpu',
    group: 'related',
    view: 'work:engines',
    fields: [work('engine_ids'), work('platforms')]
  },
  {
    key: 'series',
    guide: 'relations-and-series',
    label: '系列',
    icon: 'lucide:library',
    group: 'related',
    view: 'work:series',
    fields: [work('series_ids'), work('series_members')]
  },
  {
    key: 'relations',
    guide: 'relations-and-series',
    label: '关联作品',
    icon: 'lucide:git-fork',
    group: 'related',
    view: 'work:relations',
    fields: [work('relations')]
  },
  {
    key: 'releases',
    guide: 'releases',
    label: '发行',
    icon: 'lucide:package',
    group: 'related',
    view: 'work:releases',
    fields: [],
    always: true
  }
]

const GROUP_OF_EDIT_GROUP: Record<string, WorkspaceSectionGroup> = {
  media: 'media',
  relations: 'related',
  person: 'related',
  companies: 'related',
  traits: 'related',
  other: 'other'
}

export const FAMILY_GUIDES: Record<EditFamily, string> = {
  work: 'work-basics',
  release: 'releases',
  character: 'characters',
  person: 'credit-names-and-persons',
  credit_name: 'credit-names-and-persons',
  company: 'companies',
  tag: 'tags-and-traits',
  trait: 'tags-and-traits',
  series: 'relations-and-series',
  engine: 'companies-engines-platforms'
}

export const sectionsFromGroups = (
  family: EditFamily,
  groups: EditGroup[],
  fieldsByGroup: Record<string, string[]>
): WorkspaceSectionDef[] =>
  groups.map((group) => ({
    key: group.value,
    label: group.label,
    icon: group.icon,
    group: GROUP_OF_EDIT_GROUP[group.value] ?? 'profile',
    fields: fieldsByGroup[group.value] ?? [],
    guide: FAMILY_GUIDES[family]
  }))

export const FAMILY_SECTIONS: Partial<
  Record<EditFamily, WorkspaceSectionDef[]>
> = { work: WORK_SECTIONS }
