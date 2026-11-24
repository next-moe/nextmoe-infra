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

export const CANON_PROPERTY_LABELS: Record<string, string> = {
  medium: '媒介',
  olang: '原语言',
  status: '出版状态',
  content_rating: '分级',
  display_safety: '展示安全',
  text: '文字',
  lang: '语言',
  kind: '种类',
  machine: '机器翻译',
  position: '顺序',
  ordinal: '序号',
  label: '卷次（照录）',
  released: '发售日',
  parent_id: '上级',
  edition_kind: '版本种类',
  format: '出版形态',
  release_status: '发售状态',
  official_url: '官方页面',
  portion: '收录范围',
  category: '书的类别',
  isbn13: 'ISBN-13',
  jan: 'JAN',
  publisher_id: '出版社',
  label_id: 'レーベル',
  label_number: 'レーベル编号',
  pages: '页数',
  price_amount: '定价',
  price_currency: '币种',
  price_basis: '定价口径',
  primary_alias_id: '主名义',
  name: '名字',
  latin: '罗马字',
  role: '职责',
  alias_id: '名义',
  org_id: '组织',
  work_id: '作品',
  part_id: '分卷',
  release_id: '版本',
  person_id: '人物'
}

export const CANON_EDITABLE_ENTITIES = ['work', 'release', 'person', 'org'] as const

export const CANON_PROGRAM_MODES: Record<
  string,
  { label: string; description: string }
> = {
  review: { label: '逐条审核', description: '每条都先进待办，审核后才写入。' },
  auto: { label: '自动写入', description: '校验通过、没有身份疑问的直接写入，其余进待办。' },
  stopped: { label: '停止', description: '不再处理这个程序的数据，数据留在采集侧。' }
}

export const CANON_ACTIONS: Record<string, string> = {
  create: '新建',
  update: '修改',
  undo: '撤销',
  remove: '移除',
  merged_into: '被合并',
  absorbed: '合并进来',
  unmerge: '撤销合并',
  takedown: '下架',
  restore: '恢复'
}
