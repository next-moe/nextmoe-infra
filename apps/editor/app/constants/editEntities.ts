import type { EditEntityConfig } from '#shared/types/edit'
import { WORK_EDIT_CONFIG } from './editWork'

export type EditFamily =
  | 'work'
  | 'release'
  | 'character'
  | 'person'
  | 'credit_name'
  | 'company'
  | 'tag'
  | 'trait'
  | 'series'
  | 'engine'

export interface EntityMeta {
  family: EditFamily
  label: string
  path: string
  collection: string
  icon: string
  description: string
  searchable: boolean
  listQuery: boolean
  searchVia?: 'list'
  searchHint?: string
}

export const ENTITY_META: Record<EditFamily, EntityMeta> = {
  work: {
    family: 'work',
    label: '作品',
    path: '/works',
    collection: 'works',
    icon: 'lucide:gamepad-2',
    description: '浏览与搜索资料库中的全部作品，进入条目查看与编辑。',
    searchable: true,
    listQuery: true
  },
  release: {
    family: 'release',
    label: '发行',
    path: '/releases',
    collection: 'releases',
    icon: 'lucide:package',
    description: '作品的各个发行版本：平台、语言、发售日期与引擎。',
    searchable: true,
    listQuery: true,
    searchVia: 'list',
    searchHint: '按发行标题或所属作品的标题搜索。'
  },
  character: {
    family: 'character',
    label: '角色',
    path: '/characters',
    collection: 'characters',
    icon: 'lucide:user-round',
    description: '角色资料：别名、简介、特质、立绘与所属实例。',
    searchable: true,
    listQuery: true
  },
  person: {
    family: 'person',
    label: '人物',
    path: '/persons',
    collection: 'persons',
    icon: 'lucide:contact',
    description: '人物是署名名背后的同一个人：性别、生日、照片、简介与外链。',
    searchable: true,
    listQuery: true,
    searchVia: 'list',
    searchHint: '按人物显示名或公开的署名名搜索。'
  },
  credit_name: {
    family: 'credit_name',
    label: '署名名',
    path: '/credit-names',
    collection: 'credit-names',
    icon: 'lucide:signature',
    description: '作品职员表与配音表里出现的名字，以及它归属于哪个人物。',
    searchable: true,
    listQuery: true
  },
  company: {
    family: 'company',
    label: '厂商',
    path: '/companies',
    collection: 'companies',
    icon: 'lucide:building-2',
    description: '品牌、社团、发行商与文库：名称、别名、母子关系与 Logo。',
    searchable: true,
    listQuery: false
  },
  tag: {
    family: 'tag',
    label: '标签',
    path: '/tags',
    collection: 'tags',
    icon: 'lucide:tags',
    description: '作品标签词表。',
    searchable: true,
    listQuery: false
  },
  trait: {
    family: 'trait',
    label: '特质',
    path: '/traits',
    collection: 'traits',
    icon: 'lucide:sparkles',
    description: '角色特质词表。',
    searchable: true,
    listQuery: false
  },
  series: {
    family: 'series',
    label: '系列',
    path: '/series',
    collection: 'series',
    icon: 'lucide:library',
    description: '作品系列与其中的作品。',
    searchable: true,
    listQuery: false
  },
  engine: {
    family: 'engine',
    label: '引擎',
    path: '/engines',
    collection: 'engines',
    icon: 'lucide:cpu',
    description: '游戏引擎：名称、别名与介绍。',
    searchable: true,
    listQuery: false
  }
}

export const WRITER_LABELS: Record<string, string> = {
  user: '人工编辑',
  curated: '人工编辑',
  vndb: 'VNDB',
  bangumi: 'Bangumi',
  erogamescape: 'ErogameScape',
  dlsite: 'DLsite',
  getchu: 'Getchu',
  steam: 'Steam',
  howlongtobeat: 'HowLongToBeat',
  derived: '自动推断',
  nextmoe: 'NextMoe'
}

const BASIC = { value: 'basic', label: '基础', icon: 'lucide:settings-2' }
const NAMES = { value: 'names', label: '名称', icon: 'lucide:languages' }
const INTROS = { value: 'intros', label: '简介', icon: 'lucide:align-left' }
const MEDIA = { value: 'media', label: '图片', icon: 'lucide:image' }
const LINKS = { value: 'links', label: '外链', icon: 'lucide:link' }
const RELATIONS = { value: 'relations', label: '关联', icon: 'lucide:git-fork' }
const OTHER = { value: 'other', label: '其他', icon: 'lucide:ellipsis' }

const aliasMembers = {
  name: { label: '名称', placeholder: '别名原文' },
  lang: { label: '语言' },
  kind: { label: '类型' },
  latin: { label: '罗马字', placeholder: '可选' },
  primary: { label: '该语言主名' }
}

const introsField = (help?: string) => ({
  label: '简介',
  group: 'intros',
  control: 'intros' as const,
  help: help ?? '每种语言一段。这里写入的简介优先于资料源导入的简介显示。'
})

const linksField = {
  label: '外链',
  group: 'links',
  control: 'stringList' as const,
  help: '填写完整网址。资料源导入的外链列在下方，非身份链接可以单独隐藏。'
}

export const RELEASE_EDIT_CONFIG: EditEntityConfig = {
  family: 'release',
  entityType: 'catalog.release',
  groups: [
    BASIC,
    { value: 'companies', label: '厂商', icon: 'lucide:building-2' },
    OTHER
  ],
  fields: {
    'catalog.release.title': { label: '标题', group: 'basic' },
    'catalog.release.kind': { label: '版本类型', group: 'basic' },
    'catalog.release.lang': { label: '语言', group: 'basic' },
    'catalog.release.languages': {
      label: '支持语言',
      group: 'basic',
      help: '这个发行版本支持的全部语言。'
    },
    'catalog.release.platform': { label: '平台', group: 'basic' },
    'catalog.release.platforms': {
      label: '支持平台',
      group: 'basic',
      help: '这个发行版本对应的全部平台。'
    },
    'catalog.release.released': {
      label: '发售日期',
      group: 'basic',
      control: 'date',
      help: '只知道年份或年月时可以只填前面几项。'
    },
    'catalog.release.engine_id': {
      label: '引擎',
      group: 'basic',
      control: 'ref',
      ref: 'engine'
    },
    'catalog.release.freeware': {
      label: '免费发行',
      group: 'basic',
      control: 'switch'
    },
    'catalog.release.official': {
      label: '官方发行',
      group: 'basic',
      control: 'switch'
    },
    'catalog.release.minage': { label: '年龄限制', group: 'basic' },
    'catalog.release.labels': {
      label: '开发与发行商',
      group: 'companies',
      control: 'objectList',
      members: {
        label_id: { label: '厂商', ref: 'company' },
        kind: { label: '身份' }
      }
    },
    'catalog.release.hidden': {
      label: '隐藏这个发行',
      group: 'other',
      control: 'switch',
      help: '隐藏后这个发行不再出现在任何页面上，可以随时恢复。'
    }
  }
}

export const CHARACTER_EDIT_CONFIG: EditEntityConfig = {
  family: 'character',
  entityType: 'catalog.character',
  groups: [
    BASIC,
    { value: 'body', label: '身体数据', icon: 'lucide:ruler' },
    NAMES,
    INTROS,
    { value: 'traits', label: '特质', icon: 'lucide:sparkles' },
    MEDIA,
    RELATIONS,
    OTHER
  ],
  fields: {
    'catalog.character.display_name': { label: '显示名', group: 'basic' },
    'catalog.character.latin': { label: '罗马字', group: 'basic' },
    'catalog.character.lang': { label: '名字的语言', group: 'basic' },
    'catalog.character.gender': { label: '性别', group: 'basic' },
    'catalog.character.blood_type': { label: '血型', group: 'basic' },
    'catalog.character.birthday_month': { label: '生日（月）', group: 'basic' },
    'catalog.character.birthday_day': { label: '生日（日）', group: 'basic' },
    'catalog.character.height_cm': { label: '身高（cm）', group: 'body' },
    'catalog.character.weight_kg': { label: '体重（kg）', group: 'body' },
    'catalog.character.bust_cm': { label: '胸围（cm）', group: 'body' },
    'catalog.character.waist_cm': { label: '腰围（cm）', group: 'body' },
    'catalog.character.hip_cm': { label: '臀围（cm）', group: 'body' },
    'catalog.character.cup': { label: '罩杯', group: 'body' },
    'catalog.character.aliases': {
      label: '别名',
      group: 'names',
      control: 'objectList',
      help: '这里是人工添加的别名；资料源导入的别名列在下方，可以单独隐藏。',
      members: aliasMembers
    },
    'catalog.character.intros': introsField(),
    'catalog.character.description': {
      label: '描述',
      group: 'intros',
      control: 'textarea'
    },
    'catalog.character.traits': {
      label: '特质',
      group: 'traits',
      control: 'objectList',
      help: '人工添加或修改的特质；资料源导入的特质列在下方，可以单独隐藏。',
      members: {
        trait_id: { label: '特质', ref: 'trait' },
        spoiler: { label: '剧透' },
        lie: { label: '是谎言' }
      }
    },
    'catalog.character.image_hash': {
      label: '立绘',
      group: 'media',
      control: 'image',
      imagePreset: 'photo'
    },
    'catalog.character.figure_hash': {
      label: '去背景立绘',
      group: 'media',
      control: 'image',
      imagePreset: 'photo',
      help: '透明背景的全身立绘。替换后会清掉自动抠图使用的原图。'
    },
    'catalog.character.instance_of': {
      label: '实例归属',
      group: 'relations',
      control: 'ref',
      ref: 'character',
      help: '同一角色在其他作品里的另一份条目时，指向主条目。只能指向一层，且主条目不能再是实例。'
    }
  }
}

export const PERSON_EDIT_CONFIG: EditEntityConfig = {
  family: 'person',
  entityType: 'catalog.person',
  groups: [BASIC, MEDIA, INTROS, LINKS, OTHER],
  fields: {
    'catalog.person.primary_credit_name_id': {
      label: '主署名',
      group: 'basic',
      control: 'ref',
      ref: 'credit_name',
      help: '必须是这个人物自己的署名名之一，人物的显示名随之改变。'
    },
    'catalog.person.gender': { label: '性别', group: 'basic' },
    'catalog.person.birth_year': { label: '出生年', group: 'basic' },
    'catalog.person.birth_month': { label: '出生月', group: 'basic' },
    'catalog.person.birth_day': { label: '出生日', group: 'basic' },
    'catalog.person.photo': {
      label: '照片',
      group: 'media',
      control: 'image',
      imagePreset: 'photo'
    },
    'catalog.person.intros': introsField(),
    'catalog.person.links': {
      ...linksField,
      help: '填写完整网址。VNDB、Bangumi、EGS 的人物页会作为待确认的身份链接写入；身份链接不能隐藏，认错人请到署名名里改归属。'
    }
  }
}

export const CREDIT_NAME_EDIT_CONFIG: EditEntityConfig = {
  family: 'credit_name',
  entityType: 'catalog.credit_name',
  groups: [
    BASIC,
    { value: 'person', label: '归属人物', icon: 'lucide:contact' },
    NAMES,
    OTHER
  ],
  fields: {
    'catalog.credit_name.name': {
      label: '名称',
      group: 'basic',
      help: '改的是人物主署名时，人物的显示名也会一起改变。'
    },
    'catalog.credit_name.latin': { label: '罗马字', group: 'basic' },
    'catalog.credit_name.lang': { label: '语言', group: 'basic' },
    'catalog.credit_name.kind': { label: '类型', group: 'basic' },
    'catalog.credit_name.link_visibility': {
      label: '外链可见性',
      group: 'basic'
    },
    'catalog.credit_name.note': {
      label: '备注',
      group: 'basic',
      control: 'textarea'
    },
    'catalog.credit_name.person_id': {
      label: '归属人物',
      group: 'person',
      control: 'ref',
      ref: 'person',
      help: '把这个名字挂到某个人物下、换到另一个人物，或清空表示暂未确认。人物的最后一个署名不能移走，那种情况请合并人物。'
    },
    'catalog.credit_name.aliases': {
      label: '别名',
      group: 'names',
      control: 'objectList',
      help: '这里是人工添加的别名；资料源导入的别名列在下方，可以单独隐藏。',
      members: aliasMembers
    }
  }
}

export const COMPANY_EDIT_CONFIG: EditEntityConfig = {
  family: 'company',
  entityType: 'catalog.label',
  groups: [BASIC, NAMES, RELATIONS, MEDIA, INTROS, LINKS, OTHER],
  fields: {
    'catalog.label.name': { label: '名称', group: 'basic' },
    'catalog.label.latin': { label: '罗马字', group: 'basic' },
    'catalog.label.lang': { label: '语言', group: 'basic' },
    'catalog.label.kind': { label: '类型', group: 'basic' },
    'catalog.label.note': {
      label: '备注',
      group: 'basic',
      control: 'textarea'
    },
    'catalog.label.aliases': {
      label: '别名',
      group: 'names',
      control: 'objectList',
      help: '这里是人工添加的别名；资料源导入的别名列在下方，可以单独隐藏。',
      members: aliasMembers
    },
    'catalog.label.relations': {
      label: '厂商关系',
      group: 'relations',
      control: 'objectList',
      help: '旗下品牌、改名前身、母公司等。关系会同时写到对方厂商上。',
      members: {
        label_id: { label: '对方厂商', ref: 'company' },
        relation: { label: '关系' }
      }
    },
    'catalog.label.logo_hash': {
      label: 'Logo',
      group: 'media',
      control: 'image',
      imagePreset: 'photo'
    },
    'catalog.label.intros': introsField(),
    'catalog.label.links': linksField
  }
}

const REVIEWER_ONLY_HELP =
  '这一项决定默认列表隐藏哪些内容、SFW 页面不展示哪些内容，只有审核者可以提交修改。'

export const TAG_EDIT_CONFIG: EditEntityConfig = {
  family: 'tag',
  entityType: 'catalog.tag',
  groups: [BASIC, NAMES, RELATIONS, INTROS, OTHER],
  fields: {
    'catalog.tag.name': { label: '名称', group: 'basic' },
    'catalog.tag.kind': { label: '类型', group: 'basic' },
    'catalog.tag.tier': {
      label: '层级',
      group: 'basic',
      help: REVIEWER_ONLY_HELP
    },
    'catalog.tag.sexual': {
      label: '色情相关',
      group: 'basic',
      help: REVIEWER_ONLY_HELP
    },
    'catalog.tag.note': { label: '备注', group: 'basic', control: 'textarea' },
    'catalog.tag.parents': {
      label: '上级标签',
      group: 'relations',
      control: 'refList',
      ref: 'tag',
      help: '不能把一个标签设为它自己的祖先，形成循环的修改会被拒绝。'
    },
    'catalog.tag.intros': introsField('每种语言一段标签说明。')
  }
}

export const TRAIT_EDIT_CONFIG: EditEntityConfig = {
  family: 'trait',
  entityType: 'catalog.trait',
  groups: [BASIC, NAMES, RELATIONS, INTROS, OTHER],
  fields: {
    'catalog.trait.name': { label: '名称', group: 'basic' },
    'catalog.trait.name_zh': {
      label: '中文名',
      group: 'basic',
      help: '机器翻译的中文名可以直接改写；改写后翻译任务不会再覆盖它。'
    },
    'catalog.trait.group_id': {
      label: '所属分组',
      group: 'basic',
      control: 'ref',
      ref: 'trait',
      help: '分组本身也是一个特质（如「发色」「性格」）。'
    },
    'catalog.trait.group_order': { label: '分组内排序', group: 'basic' },
    'catalog.trait.default_spoiler': { label: '默认剧透等级', group: 'basic' },
    'catalog.trait.applicable': { label: '可直接标注', group: 'basic' },
    'catalog.trait.searchable': { label: '可用于搜索', group: 'basic' },
    'catalog.trait.sexual': {
      label: '色情相关',
      group: 'basic',
      help: REVIEWER_ONLY_HELP
    },
    'catalog.trait.aliases': {
      label: '别名',
      group: 'names',
      control: 'stringList'
    },
    'catalog.trait.parents': {
      label: '上级特质',
      group: 'relations',
      control: 'refList',
      ref: 'trait',
      help: '不能把一个特质设为它自己的祖先，形成循环的修改会被拒绝。'
    },
    'catalog.trait.description': {
      label: '说明',
      group: 'intros',
      control: 'textarea'
    },
    'catalog.trait.description_zh': {
      label: '中文说明',
      group: 'intros',
      control: 'textarea',
      help: '机器翻译的中文说明可以直接改写；改写后翻译任务不会再覆盖它。'
    }
  }
}

export const SERIES_EDIT_CONFIG: EditEntityConfig = {
  family: 'series',
  entityType: 'catalog.series',
  groups: [BASIC, INTROS, OTHER],
  fields: {
    'catalog.series.name': {
      label: '名称',
      group: 'basic',
      help: '只有人工维护的系列可以改名；资料源生成的系列名称由导入流程决定。'
    },
    'catalog.series.intros': introsField('每种语言一段系列介绍。')
  }
}

export const ENGINE_EDIT_CONFIG: EditEntityConfig = {
  family: 'engine',
  entityType: 'catalog.engine',
  groups: [BASIC, INTROS, OTHER],
  fields: {
    'catalog.engine.name': { label: '名称', group: 'basic' },
    'catalog.engine.aliases': {
      label: '别名',
      group: 'basic',
      control: 'stringList'
    },
    'catalog.engine.intro': {
      label: '介绍',
      group: 'intros',
      control: 'textarea'
    }
  }
}

export const EDIT_CONFIGS: Record<EditFamily, EditEntityConfig> = {
  work: WORK_EDIT_CONFIG,
  release: RELEASE_EDIT_CONFIG,
  character: CHARACTER_EDIT_CONFIG,
  person: PERSON_EDIT_CONFIG,
  credit_name: CREDIT_NAME_EDIT_CONFIG,
  company: COMPANY_EDIT_CONFIG,
  tag: TAG_EDIT_CONFIG,
  trait: TRAIT_EDIT_CONFIG,
  series: SERIES_EDIT_CONFIG,
  engine: ENGINE_EDIT_CONFIG
}

export const isEditFamily = (family: string): family is EditFamily =>
  family in EDIT_CONFIGS
