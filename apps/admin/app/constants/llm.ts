import type { KunTabItem } from '@kungal/ui-vue'
import type { AiChipColor } from '~/constants/ai'
import type {
  LlmCatalogKind,
  LlmCatalogKindSource,
  LlmImportAction,
  LlmImportKind,
  LlmModelKind,
  LlmPlanStepState,
  LlmPriceKey,
  LlmRouteHealthState,
  LlmRouteStatus,
  LlmSceneChainKey,
  LlmScenePutRequest,
  LlmSceneClass,
  LlmSceneStrategy,
  LlmUsageDays,
  LlmUsageGroup
} from '~~/shared/types/llm'

export const LLM_API = '/admin/ai/llm'

export const LLM_TABS: KunTabItem[] = [
  { value: 'providers', textValue: '供应商' },
  { value: 'models', textValue: '模型' },
  { value: 'routes', textValue: '路由' },
  { value: 'scenes', textValue: '场景' },
  { value: 'health', textValue: '健康' },
  { value: 'usage', textValue: '用量' },
  { value: 'keys', textValue: '访问密钥' },
  { value: 'events', textValue: '事件' },
  { value: 'config', textValue: '配置' }
]

export const LLM_MODEL_KIND_META: Record<
  LlmModelKind,
  { label: string; color: AiChipColor }
> = {
  chat: { label: '对话', color: 'primary' },
  embedding: { label: '向量', color: 'info' },
  moderation: { label: '审核', color: 'warning' },
  search: { label: '联网搜索', color: 'secondary' },
  image: { label: '图片生成', color: 'success' }
}

export const LLM_MODEL_KIND_OPTIONS = (
  Object.keys(LLM_MODEL_KIND_META) as LlmModelKind[]
).map((value) => ({
  value,
  label: `${value} · ${LLM_MODEL_KIND_META[value].label}`
}))

export const LLM_CATALOG_KIND_META: Record<
  LlmCatalogKind,
  { label: string; color: AiChipColor }
> = {
  ...LLM_MODEL_KIND_META,
  video: { label: '视频生成', color: 'danger' },
  speech: { label: '语音合成', color: 'default' },
  transcription: { label: '语音转写', color: 'default' },
  rerank: { label: '重排序', color: 'default' }
}

export const LLM_CATALOG_KINDS = Object.keys(
  LLM_CATALOG_KIND_META
) as LlmCatalogKind[]

export const LLM_CATALOG_KIND_SOURCE_LABEL: Record<
  LlmCatalogKindSource,
  string
> = {
  metadata: '元数据',
  name: '按名称推断',
  admin: '手动设置',
  probe: '探测确认'
}

export const LLM_CATALOG_STATUS_TABS = [
  { value: 'listed', label: '在列' },
  { value: 'delisted', label: '已下架' },
  { value: 'all', label: '全部' }
] as const

export const LLM_CATALOG_IMAGE_PROBE_COST = '约 $0.02'

export const LLM_PRICE_FIELDS: { key: LlmPriceKey; label: string }[] = [
  { key: 'input_micro', label: '输入 / 1M tokens' },
  { key: 'cached_micro', label: '缓存命中输入 / 1M tokens' },
  { key: 'output_micro', label: '输出 / 1M tokens' },
  { key: 'per_call_micro', label: '每次调用' }
]

export const LLM_MODEL_CAPABILITIES = [
  { key: 'tool_call', label: '工具调用' },
  { key: 'vision', label: '视觉' },
  { key: 'reasoning', label: '推理' }
] as const

export const LLM_ROUTE_STATUS_META: Record<
  LlmRouteStatus,
  { label: string; color: AiChipColor }
> = {
  active: { label: '启用', color: 'success' },
  disabled: { label: '停用', color: 'default' }
}

export const LLM_ROUTE_STATUS_OPTIONS = (
  Object.keys(LLM_ROUTE_STATUS_META) as LlmRouteStatus[]
).map((value) => ({
  value,
  label: LLM_ROUTE_STATUS_META[value].label
}))

export const LLM_ROUTE_HINT =
  '优先级高的档位先试；同一档位内按权重分流；某条路由在首个 token 之前失败时，自动切到下一条。'

export const LLM_SCENE_CLASS_META: Record<LlmSceneClass, string> = {
  interactive: '交互',
  search: '检索',
  batch: '批处理',
  moderation: '审核'
}

export const LLM_SCENE_CLASS_OPTIONS = (
  Object.keys(LLM_SCENE_CLASS_META) as LlmSceneClass[]
).map((value) => ({
  value,
  label: `${value} · ${LLM_SCENE_CLASS_META[value]}`
}))

export const LLM_SCENE_STRATEGY_META: Record<
  LlmSceneStrategy,
  { label: string; option: string; hint: string }
> = {
  weighted: {
    label: '加权',
    option: '加权（同一会话固定路由）',
    hint: '同一档位内按权重分流;同一会话始终落在同一条路由,利于上游缓存'
  },
  least_busy: {
    label: '最少在途',
    option: '最少在途',
    hint: '优先选本进程里进行中调用最少的路由'
  },
  latency: {
    label: '最低延迟',
    option: '最低延迟',
    hint: '优先选近期首字最快的路由,约一成调用用于探索其它路由'
  },
  cost: {
    label: '最低价格',
    option: '最低价格',
    hint: '优先选标价最便宜的路由'
  }
}

export const LLM_SCENE_STRATEGY_OPTIONS = (
  Object.keys(LLM_SCENE_STRATEGY_META) as LlmSceneStrategy[]
).map((value) => ({ value, label: LLM_SCENE_STRATEGY_META[value].option }))

export const LLM_SCENE_CHAINS: {
  key: LlmSceneChainKey
  label: string
  hint: string
}[] = [
  {
    key: 'fallback_model_ids',
    label: '备选模型',
    hint: '主模型的路由全部失败后按顺序尝试'
  },
  {
    key: 'context_fallback_model_ids',
    label: '上下文超长时改用',
    hint: '模型以输入过长拒绝时按顺序改用'
  },
  {
    key: 'content_fallback_model_ids',
    label: '被内容过滤时改用',
    hint: '模型的内容过滤拒绝请求(尚未输出)时按顺序改用'
  }
]

export const LLM_SCENE_DEFAULTS: LlmScenePutRequest = {
  model_id: 0,
  fallback_model_ids: [],
  context_fallback_model_ids: [],
  content_fallback_model_ids: [],
  strategy: 'weighted',
  daily_budget_micro: 0,
  class: 'interactive',
  temperature: null,
  max_output: 0,
  ttft_ms: 20000,
  idle_ms: 30000,
  total_ms: 120000
}

export const LLM_PLAN_STATE_META: Record<
  LlmPlanStepState,
  { label: string; color: AiChipColor }
> = {
  ready: { label: '就绪', color: 'success' },
  cooling: { label: '冷却中', color: 'warning' },
  forced: { label: '强制尝试', color: 'danger' }
}

export const LLM_HEALTH_STATE_META: Record<
  LlmRouteHealthState,
  { label: string; color: AiChipColor }
> = {
  healthy: { label: '健康', color: 'success' },
  cooling: { label: '冷却中', color: 'warning' },
  limited: { label: '已限流', color: 'secondary' },
  disabled: { label: '停用', color: 'default' }
}

export const LLM_HEALTH_REFRESH_MS = 10_000

export const LLM_BUDGET_HINT =
  '按北京时间每天重置;0 或留空 = 不限。超出后的调用返回 budget_exceeded(/v1 上是 HTTP 429)。'

export const LLM_BUDGET_WARN_RATIO = 0.8

export const LLM_USAGE_DAYS_TABS: KunTabItem[] = [
  { value: '1', textValue: '今天' },
  { value: '7', textValue: '7 天' },
  { value: '30', textValue: '30 天' }
]

export const LLM_USAGE_DAYS: LlmUsageDays[] = [1, 7, 30]

export const LLM_USAGE_GROUP_LABEL: Record<LlmUsageGroup, string> = {
  provider: '供应商',
  route: '路由',
  scene: '场景',
  key: '访问密钥',
  model: '模型'
}

export const LLM_USAGE_GROUPS = Object.keys(
  LLM_USAGE_GROUP_LABEL
) as LlmUsageGroup[]

export const LLM_USAGE_GROUP_TABS: KunTabItem[] = LLM_USAGE_GROUPS.map(
  (value) => ({ value, textValue: `按${LLM_USAGE_GROUP_LABEL[value]}` })
)

export const LLM_IMPORT_KIND_LABEL: Record<LlmImportKind, string> = {
  provider: '供应商',
  model: '模型',
  route: '路由',
  scene: '场景',
  access_key: '访问密钥'
}

export const LLM_IMPORT_ACTION_META: Record<
  LlmImportAction,
  { label: string; color: AiChipColor }
> = {
  create: { label: '新增', color: 'success' },
  update: { label: '更新', color: 'warning' },
  unchanged: { label: '不变', color: 'default' }
}

export const LLM_ACCESS_KEY_HINT =
  '内部调用方使用 base URL http://ai:9284/v1(生产内网),请求头带 Authorization: Bearer <secret>。'

export const LLM_EVENT_KIND_LABEL: Record<string, string> = {
  auth_failing: '鉴权/额度持续失败'
}

export const LLM_EVENT_KIND_COLOR: Record<string, AiChipColor> = {
  auth_failing: 'danger',
  created: 'success',
  saved: 'success',
  updated: 'info',
  deleted: 'warning'
}

export const llmEventColor = (kind: string): AiChipColor =>
  LLM_EVENT_KIND_COLOR[kind] ??
  LLM_EVENT_KIND_COLOR[kind.slice(kind.lastIndexOf('_') + 1)] ??
  'default'

export const LLM_EVENTS_LIMIT = 100
