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
    { value: 'relations', label: '关联作品', icon: 'lucide:git-fork' },
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
    'catalog.work.route_hints': {
      label: '游戏自带路线提示',
      group: 'basic',
      control: 'switch',
      help: '游戏里能打开路线或选项提示（例如 2024 年的 ToHeart 重制版）时打开。攻略页会先提示读者可以使用游戏自带的提示，再让读者选择是否查看攻略。'
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
      help: '每种语言一段。人工撰写的简介优先显示；没有时显示资料源的版本。'
    },
    'catalog.work.tag_ids': {
      label: '标签',
      group: 'tags',
      control: 'refList',
      ref: 'tag',
      help: '人工添加的标签可以移除；资料源导入的标签可以隐藏。'
    },
    'catalog.work.tag_spoilers': {
      label: '标签剧透等级',
      group: 'tags',
      control: 'objectList',
      emptyText: '还没有人工设置剧透等级的标签。',
      help: '为这部作品上的某个标签设置剧透等级，会同时作用于该标签来自各资料源的记录，资料源再次同步也不会覆盖。',
      members: {
        tag_id: { label: '标签', ref: 'tag' },
        spoiler: { label: '剧透等级' }
      }
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
    'catalog.work.platforms': {
      label: '平台',
      group: 'companies',
      control: 'vocabList',
      vocabulary: 'platform',
      help: '作品层面的平台（来自 Bangumi、ErogameScape 等）。各发行版本自己的平台在发行页编辑。'
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
    'catalog.work.series_members': {
      label: '系列内位置',
      group: 'series',
      control: 'objectList',
      fixedRows: true,
      emptyText: '这部作品还不属于任何系列。',
      help: '调整这部作品在各系列中的序号与身份（正篇、FD、外传等）。加入或退出系列请编辑上面的「系列」。',
      members: {
        series_id: { label: '系列', ref: 'series', readonly: true },
        position: { label: '序号' },
        kind: { label: '身份' }
      }
    },
    'catalog.work.relations': {
      label: '关联作品',
      group: 'relations',
      control: 'objectList',
      help: '续作、外传、FD、重制等关系。关系按「本作 → 对方」来读：本作是对方的续作选「是…的续作」，对方是本作的续作选「有续作」。同系列、同世界观、联动不分方向。',
      members: {
        work_id: { label: '对方作品', ref: 'work' },
        relation: { label: '关系' },
        reverse: { label: '反向' },
        note: { label: '备注', placeholder: '可选' }
      }
    },
    'catalog.work.links': {
      label: '外链',
      group: 'links',
      control: 'stringList',
      help: '填写完整网址。VNDB、Bangumi 的作品页会记为待确认的身份链接，其他网站（包括批评空间）记为相关链接。'
    },
    'catalog.work.covers': {
      label: '封面',
      group: 'media',
      control: 'images',
      imagePreset: 'cover'
    },
    'catalog.work.covers_upstream': {
      label: '资料源封面',
      group: 'media',
      control: 'regrade'
    },
    'catalog.work.cover_order': {
      label: '封面顺序',
      group: 'media',
      control: 'readonly'
    },
    'catalog.work.screenshots': {
      label: '截图',
      group: 'media',
      control: 'images',
      imagePreset: 'screenshot'
    },
    'catalog.work.screenshots_upstream': {
      label: '资料源截图',
      group: 'media',
      control: 'regrade'
    },
    'catalog.work.screenshot_order': {
      label: '截图顺序',
      group: 'media',
      control: 'readonly'
    },
    'catalog.work.credits': {
      label: '制作人员',
      group: 'credits',
      control: 'objectList',
      help: '人工添加和资料源导入的署名列在同一张表里。资料源导入的可以直接改备注、剧透、发行和厂商，也可以单独隐藏。',
      members: {
        role_id: { label: '职位', ref: 'role' },
        credit_name_id: { label: '署名', ref: 'credit_name' },
        character_id: { label: '配音角色', ref: 'character' },
        note: { label: '备注', placeholder: '可选' },
        spoiler: { label: '剧透等级' },
        release_id: { label: '仅限发行', ref: 'release' },
        label_id: { label: '所属厂商', ref: 'company' }
      }
    },
    'catalog.work.credits_upstream': {
      label: '资料源署名',
      group: 'credits',
      control: 'objectList',
      members: {
        role_id: { label: '职位', ref: 'role' },
        credit_name_id: { label: '署名', ref: 'credit_name' },
        character_id: { label: '配音角色', ref: 'character' },
        note: { label: '备注' },
        spoiler: { label: '剧透等级' },
        release_id: { label: '仅限发行', ref: 'release' },
        label_id: { label: '所属厂商', ref: 'company' }
      }
    },
    'catalog.work.roster': {
      label: '登场角色',
      group: 'roster',
      control: 'roster',
      help: '可以调整每个角色的出场程度与剧透等级，也可以添加缺失的角色；不属于这部作品的角色可以隐藏。'
    }
  }
}
