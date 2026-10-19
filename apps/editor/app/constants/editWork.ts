import type { EditEntityConfig } from '#shared/types/edit'

export const WORK_EDIT_CONFIG: EditEntityConfig = {
  family: 'work',
  entityType: 'catalog.work',
  groups: [
    { value: 'basic', label: '基础', icon: 'lucide:settings-2' },
    { value: 'titles', label: '标题', icon: 'lucide:heading' },
    { value: 'intros', label: '简介', icon: 'lucide:align-left' },
    { value: 'tags', label: '标签', icon: 'lucide:tags' },
    { value: 'companies', label: '厂商与引擎', icon: 'lucide:building-2' },
    { value: 'series', label: '系列', icon: 'lucide:library' },
    { value: 'links', label: '外链', icon: 'lucide:link' },
    { value: 'media', label: '媒体', icon: 'lucide:images' },
    { value: 'credits', label: '职员', icon: 'lucide:clapperboard' },
    { value: 'roster', label: '角色', icon: 'lucide:users' },
    { value: 'other', label: '其他', icon: 'lucide:ellipsis' }
  ],
  fields: {
    'catalog.work.display_name': {
      label: '显示名',
      group: 'basic',
      help: '列表与页面标题使用的名称。修改标题后它会按原语言的正式名自动重算，一般无需单独修改。'
    },
    'catalog.work.olang': {
      label: '原语言',
      group: 'basic',
      help: '作品最初发行时使用的语言。'
    },
    'catalog.work.content_rating': {
      label: '分级',
      group: 'basic'
    },
    'catalog.work.display_nsfw': {
      label: '按 NSFW 内容显示',
      group: 'basic',
      control: 'switch',
      help: '对已被站点收录的作品生效：打开后这部作品按 NSFW 内容处理，只向开启了 NSFW 显示的用户展示。'
    },
    'catalog.work.titles': {
      label: '标题',
      group: 'titles',
      control: 'titles',
      help: '删除一个已有标题时会同时屏蔽它，资料源下次同步也不会再把它加回来。',
      members: {
        lang: { label: '语言' },
        title: { label: '标题', placeholder: '标题原文' },
        kind: { label: '类型' },
        latin: { label: '罗马字', placeholder: '可选' }
      }
    },
    'catalog.work.intros': {
      label: '简介',
      group: 'intros',
      control: 'intros',
      help: '每种语言一段。这里写入的简介优先于资料源导入的简介显示。'
    },
    'catalog.work.tag_ids': {
      label: '标签',
      group: 'tags',
      control: 'refList',
      ref: 'tag',
      help: '这里是人工添加的标签；资料源导入的标签列在下方，可以单独隐藏。'
    },
    'catalog.work.labels': {
      label: '厂商',
      group: 'companies',
      control: 'objectList',
      upstreamTitle: '当前展示的厂商（含资料源导入）',
      members: {
        label_id: { label: '厂商', ref: 'company' },
        kind: { label: '身份' }
      }
    },
    'catalog.work.engine_ids': {
      label: '引擎',
      group: 'companies',
      control: 'refList',
      ref: 'engine'
    },
    'catalog.work.series_ids': {
      label: '系列',
      group: 'series',
      control: 'refList',
      ref: 'series',
      help: '只能加入人工维护的系列；资料源生成的系列由导入流程管理。'
    },
    'catalog.work.links': {
      label: '外链',
      group: 'links',
      control: 'stringList',
      help: '填写完整网址。已知资料站（VNDB、Bangumi 等）的作品页会被识别为身份链接。'
    },
    'catalog.work.covers': {
      label: '封面',
      group: 'media',
      control: 'images',
      imagePreset: 'cover'
    },
    'catalog.work.covers_upstream': {
      label: '资料源封面分级',
      group: 'media',
      control: 'regrade'
    },
    'catalog.work.screenshots': {
      label: '截图',
      group: 'media',
      control: 'images',
      imagePreset: 'screenshot'
    },
    'catalog.work.screenshots_upstream': {
      label: '资料源截图分级',
      group: 'media',
      control: 'regrade'
    },
    'catalog.work.credits': {
      label: '制作人员',
      group: 'credits',
      control: 'objectList',
      help: '这里是人工添加的署名；资料源导入的署名列在下方，可以单独隐藏。',
      members: {
        role_id: { label: '职位', ref: 'role' },
        credit_name_id: { label: '署名', ref: 'credit_name' },
        character_id: { label: '配音角色', ref: 'character' },
        note: { label: '备注', placeholder: '可选' }
      }
    },
    'catalog.work.roster': {
      label: '登场角色',
      group: 'roster',
      control: 'roster',
      help: '可以调整每个角色的出场程度与剧透等级；不属于这部作品的角色可以隐藏。'
    }
  }
}
