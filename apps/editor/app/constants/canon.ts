import type { CanonTodoKind } from '#shared/types/canon'

export const CANON_TODO_KINDS: Record<
  CanonTodoKind,
  { label: string; description: string; permission: string }
> = {
  first_write: {
    label: '首轮审核',
    description:
      '采集程序还在首轮审核阶段，证据已经校验过。接受即按程序的名义写入，并记上你是审核人；字段不会被锁定。',
    permission: 'canon.todo.value'
  },
  identity: {
    label: '身份',
    description:
      '采集记录可能是某个已有条目。每个问题都要选：是哪一个，或者都不是。选旧库里的条目时，新条目会沿用它的编号。',
    permission: 'canon.todo.identity'
  },
  conflict: {
    label: '来源矛盾',
    description: '两个同等级的来源说法不同。接受即改用这次的值，驳回则保留现值。',
    permission: 'canon.todo.value'
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
