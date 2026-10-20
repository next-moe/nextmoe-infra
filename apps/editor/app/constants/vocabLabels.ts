import {
  ATTRIBUTION_ROLE_LABELS,
  MEDIUM_LABELS,
  RELEASE_STATUS_LABELS,
  ROSTER_ROLE_LABELS,
  TITLE_KIND_LABELS
} from './catalog'
import { RELEASE_KIND_LABELS, SERIES_MEMBER_KIND_LABELS } from './related'

const WORK_RELATION_LABELS: Record<string, string> = {
  adaptation_of: '改编自',
  sequel_of: '续作',
  side_story_of: '外传',
  fandisc_of: 'Fandisc',
  collects: '收录',
  remake_of: '重制',
  same_series: '同系列',
  same_setting: '同世界观',
  crossover_with: '联动',
  shares_character: '角色出演',
  alternative_setting: '不同世界观',
  alternative_version: '不同演绎'
}

const COMPANY_RELATION_LABELS: Record<string, string> = {
  parent: '母公司',
  subsidiary: '子公司',
  imprint: '旗下品牌',
  imprint_of: '所属品牌方',
  spawned: '分出',
  origin: '前身来源',
  succeeded_by: '由…继承',
  formerly: '曾用名'
}

export const EDIT_LANGUAGE_LABELS: Record<string, string> = {
  ja: '日语',
  'zh-Hans': '简体中文',
  'zh-Hant': '繁体中文',
  zh: '中文',
  en: '英语',
  ko: '韩语',
  ar: '阿拉伯语',
  be: '白俄罗斯语',
  bg: '保加利亚语',
  ca: '加泰罗尼亚语',
  cs: '捷克语',
  da: '丹麦语',
  de: '德语',
  el: '希腊语',
  eo: '世界语',
  es: '西班牙语',
  fi: '芬兰语',
  fr: '法语',
  ga: '爱尔兰语',
  gd: '苏格兰盖尔语',
  he: '希伯来语',
  hi: '印地语',
  hr: '克罗地亚语',
  hu: '匈牙利语',
  id: '印尼语',
  it: '意大利语',
  iu: '因纽特语',
  la: '拉丁语',
  lt: '立陶宛语',
  lv: '拉脱维亚语',
  mk: '马其顿语',
  ms: '马来语',
  nl: '荷兰语',
  no: '挪威语',
  pl: '波兰语',
  'pt-br': '葡萄牙语（巴西）',
  'pt-pt': '葡萄牙语（葡萄牙）',
  ro: '罗马尼亚语',
  ru: '俄语',
  sk: '斯洛伐克语',
  sl: '斯洛文尼亚语',
  sr: '塞尔维亚语',
  sv: '瑞典语',
  ta: '泰米尔语',
  th: '泰语',
  tr: '土耳其语',
  uk: '乌克兰语',
  ur: '乌尔都语',
  vi: '越南语',
  ck: '切罗基语',
  et: '爱沙尼亚语'
}

export const VOCAB_LABELS: Record<string, Record<string, string>> = {
  olang: EDIT_LANGUAGE_LABELS,
  intro_lang: EDIT_LANGUAGE_LABELS,
  release_lang: EDIT_LANGUAGE_LABELS,
  content_rating: { all_ages: '全年龄', sensitive: '敏感', r18: 'R18' },
  sexual: { safe: '安全', suggestive: '擦边', explicit: '露骨' },
  violence: { tame: '无', violent: '暴力', brutal: '血腥' },
  spoiler: { none: '无剧透', minor: '轻微剧透', major: '严重剧透' },
  title_kind: TITLE_KIND_LABELS,
  attribution_role: ATTRIBUTION_ROLE_LABELS,
  roster_role: ROSTER_ROLE_LABELS,
  company_kind: {
    game_brand: '游戏品牌',
    bunko: '文库',
    publisher: '出版社',
    anime_studio: '动画公司',
    doujin_circle: '同人社团',
    group: '团体'
  },
  gender: { male: '男', female: '女', other: '其他' },
  medium: MEDIUM_LABELS,
  release_status: RELEASE_STATUS_LABELS,
  release_kind: RELEASE_KIND_LABELS,
  member_kind: SERIES_MEMBER_KIND_LABELS,
  work_relation: WORK_RELATION_LABELS,
  relation_types: { ...WORK_RELATION_LABELS, ...COMPANY_RELATION_LABELS },
  company_relation: COMPANY_RELATION_LABELS,
  alias_kind: { translation: '译名', spelling_variant: '异写' },
  credit_name_kind: {
    main: '本名',
    pen_name: '笔名',
    distinct_persona: '独立马甲',
    former_name: '曾用名'
  },
  link_visibility: { public: '公开', hidden: '隐藏' },
  tag_kind: { content: '内容', meta: '元信息' },
  tier: { core: '核心', longtail: '长尾', hidden: '隐藏' },
  content_limit: { sfw: '全年龄', nsfw: 'NSFW' },
  claim_state: {
    live: '已上线',
    draft: '草稿',
    pending: '待审核',
    declined: '已拒绝',
    hidden: '已隐藏'
  }
}

export const vocabLabel = (
  vocabulary: string,
  token: string,
  fallback?: string
) => VOCAB_LABELS[vocabulary]?.[token] || fallback || token
