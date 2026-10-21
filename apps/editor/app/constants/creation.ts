import type { EditRefFamily } from '#shared/types/edit'
import type { EditFamily } from './editEntities'

export const CREATABLE_FAMILIES: EditFamily[] = [
  'character',
  'company',
  'credit_name',
  'person',
  'release',
  'series',
  'engine',
  'tag',
  'trait'
]

export const MERGEABLE_FAMILIES: EditFamily[] = [
  'work',
  'company',
  'character',
  'person',
  'credit_name'
]

export const CREATION_EXTRA_FIELDS: Partial<Record<EditFamily, string[]>> = {
  character: ['catalog.character.latin', 'catalog.character.lang'],
  company: ['catalog.label.latin', 'catalog.label.lang', 'catalog.label.kind'],
  credit_name: ['catalog.credit_name.latin', 'catalog.credit_name.lang'],
  release: ['catalog.release.title', 'catalog.release.lang'],
  trait: ['catalog.trait.group_id']
}

export const CREATION_PARAM_LABELS: Record<
  string,
  { label: string; ref: EditRefFamily; help: string }
> = {
  work_id: {
    label: '所属作品',
    ref: 'work',
    help: '发行总是属于一部已有的作品。'
  },
  credit_name_id: {
    label: '署名名',
    ref: 'credit_name',
    help: '新人物从一个还没有归属的署名名诞生，这个名字会成为人物的主名。'
  }
}

export const MERGE_STATE_LABELS: Record<string, string> = {
  open: '待审核',
  approved: '已批准，冷却中',
  executed: '已合并',
  rejected: '已驳回',
  withdrawn: '已撤回',
  undone: '已撤销'
}
