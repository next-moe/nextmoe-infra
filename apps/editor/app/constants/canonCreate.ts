export interface CanonCreatePart {
  ref: string
  entity: string
  /** Fields shown, in order. */
  fields: string[]
  required: string[]
  /** Fixed values; `@x` takes x from the context the form was opened in. */
  preset: Record<string, string>
  defaults?: Record<string, string>
}

export interface CanonCreateTemplate {
  key: string
  label: string
  /** Opens this entity when done; the created ref's id, or the context's. */
  open?: { entity: string; ref: string }
  parts: CanonCreatePart[]
}

const EDITION: CanonCreatePart[] = [
  {
    ref: 'release',
    entity: 'release',
    fields: ['lang', 'format', 'edition_kind', 'released', 'release_status', 'official_url'],
    required: ['lang', 'format'],
    preset: {},
    defaults: { lang: 'ja', format: 'paperback', edition_kind: 'standard', release_status: 'released' }
  },
  {
    ref: 'book',
    entity: 'book_release',
    fields: ['isbn13', 'label_id', 'publisher_id', 'label_number', 'pages', 'price_amount', 'price_currency', 'price_basis'],
    required: [],
    preset: { release_id: 'ref:release' }
  }
]

export const CANON_CREATE_TEMPLATES: Record<string, CanonCreateTemplate> = {
  work: {
    key: 'work',
    label: '新建作品',
    open: { entity: 'work', ref: 'work' },
    parts: [
      {
        ref: 'work',
        entity: 'work',
        fields: ['medium', 'olang', 'status', 'content_rating'],
        required: ['medium'],
        preset: {},
        defaults: { medium: 'novel', olang: 'ja', status: 'unknown', content_rating: 'all_ages' }
      },
      {
        ref: 'title',
        entity: 'work_title',
        fields: ['text', 'lang', 'kind'],
        required: ['text', 'lang', 'kind'],
        preset: { work_id: 'ref:work' },
        defaults: { lang: 'ja', kind: 'official' }
      },
      {
        ref: 'book',
        entity: 'book_work',
        fields: ['category'],
        required: ['category'],
        preset: { work_id: 'ref:work' },
        defaults: { category: 'light_novel' }
      }
    ]
  },
  person: {
    key: 'person',
    label: '新建人物',
    open: { entity: 'person', ref: 'person' },
    parts: [
      { ref: 'person', entity: 'person', fields: [], required: [], preset: {} },
      {
        ref: 'alias',
        entity: 'person_alias',
        fields: ['name', 'latin', 'lang'],
        required: ['name'],
        preset: { person_id: 'ref:person' },
        defaults: { lang: 'ja' }
      }
    ]
  },
  org: {
    key: 'org',
    label: '新建组织',
    open: { entity: 'org', ref: 'org' },
    parts: [
      {
        ref: 'org',
        entity: 'org',
        fields: ['kind', 'parent_id'],
        required: ['kind'],
        preset: {},
        defaults: { kind: 'label' }
      },
      {
        ref: 'name',
        entity: 'org_name',
        fields: ['text', 'lang'],
        required: ['text', 'lang'],
        preset: { org_id: 'ref:org' },
        defaults: { lang: 'ja' }
      }
    ]
  },
  work_title: {
    key: 'work_title',
    label: '加标题',
    parts: [
      {
        ref: 'title',
        entity: 'work_title',
        fields: ['text', 'lang', 'kind'],
        required: ['text', 'lang', 'kind'],
        preset: { work_id: '@work' },
        defaults: { lang: 'zh-Hans', kind: 'translated' }
      }
    ]
  },
  credit: {
    key: 'credit',
    label: '加署名',
    parts: [
      {
        ref: 'credit',
        entity: 'credit',
        fields: ['alias_id', 'role'],
        required: ['alias_id', 'role'],
        preset: { work_id: '@work' },
        defaults: { role: 'author' }
      }
    ]
  },
  volume: {
    key: 'volume',
    label: '加一卷和它的版本',
    parts: [
      {
        ref: 'part',
        entity: 'work_part',
        fields: ['kind', 'label', 'ordinal', 'released'],
        required: ['kind'],
        preset: { work_id: '@work' },
        defaults: { kind: 'volume' }
      },
      ...EDITION,
      {
        ref: 'content',
        entity: 'release_content',
        fields: [],
        required: [],
        preset: { release_id: 'ref:release', work_id: '@work', part_id: 'ref:part' }
      }
    ]
  },
  edition: {
    key: 'edition',
    label: '给已有的卷加版本',
    parts: [
      ...EDITION,
      {
        ref: 'content',
        entity: 'release_content',
        fields: ['part_id', 'portion'],
        required: ['part_id'],
        preset: { release_id: 'ref:release', work_id: '@work' },
        defaults: { portion: 'full' }
      }
    ]
  },
  person_alias: {
    key: 'person_alias',
    label: '加名义',
    parts: [
      {
        ref: 'alias',
        entity: 'person_alias',
        fields: ['name', 'latin', 'lang'],
        required: ['name'],
        preset: { person_id: '@person' },
        defaults: { lang: 'ja' }
      }
    ]
  },
  org_name: {
    key: 'org_name',
    label: '加名称',
    parts: [
      {
        ref: 'name',
        entity: 'org_name',
        fields: ['text', 'lang'],
        required: ['text', 'lang'],
        preset: { org_id: '@org' },
        defaults: { lang: 'ja' }
      }
    ]
  }
}

export const CANON_CREATE_ROOTS = ['work', 'person', 'org'] as const

export const CANON_CREATE_CHILDREN: Record<string, string[]> = {
  work: ['work_title', 'volume', 'edition', 'credit'],
  person: ['person_alias'],
  org: ['org_name'],
  release: []
}
