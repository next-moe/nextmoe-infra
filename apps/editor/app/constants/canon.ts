import type { CanonTodoKind } from '#shared/types/canon'

export const CANON_TODO_KINDS: Record<
  CanonTodoKind,
  { label: string; description: string; permission: string }
> = {
  identity: {
    label: '身份',
    description: '采集记录认不出是哪个已有条目；填已有条目的编号即认定为同一个，留空则新建。',
    permission: 'canon.todo.identity'
  },
  agent_on_human: {
    label: '取值',
    description: '采集到的值撞上了人写过的字段；接受即以你的名义改成采集值。',
    permission: 'canon.todo.value'
  }
}

export const CANON_ENTITY_LABELS: Record<string, string> = {
  work: '作品',
  work_title: '作品标题',
  work_part: '分卷',
  release: '版本',
  release_content: '版本收录',
  book_work: '书目',
  book_release: '书的版本',
  person: '人物',
  person_alias: '人物别名',
  org: '组织',
  org_name: '组织名',
  credit: '署名'
}

export const CANON_CITABLE_ENTITIES = ['work', 'person', 'person_alias', 'org', 'org_name']
