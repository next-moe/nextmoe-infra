import type { KunTabItem } from '@kungal/ui-vue'
import type { AiChipColor } from '~/constants/ai'
import type {
  LlmModelKind,
  LlmRouteStatus,
  LlmScenePutRequest,
  LlmSceneClass
} from '~~/shared/types/llm'

export const LLM_API = '/admin/ai/llm'

export const LLM_TABS: KunTabItem[] = [
  { value: 'providers', textValue: '供应商' },
  { value: 'models', textValue: '模型' },
  { value: 'routes', textValue: '路由' },
  { value: 'scenes', textValue: '场景' },
  { value: 'keys', textValue: '访问密钥' },
  { value: 'events', textValue: '事件' }
]

export const LLM_MODEL_KIND_META: Record<
  LlmModelKind,
  { label: string; color: AiChipColor }
> = {
  chat: { label: '对话', color: 'primary' },
  embedding: { label: '向量', color: 'info' },
  moderation: { label: '审核', color: 'warning' }
}

export const LLM_MODEL_KIND_OPTIONS = (
  Object.keys(LLM_MODEL_KIND_META) as LlmModelKind[]
).map((value) => ({
  value,
  label: `${value} · ${LLM_MODEL_KIND_META[value].label}`
}))

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

export const LLM_SCENE_DEFAULTS: LlmScenePutRequest = {
  model_id: 0,
  fallback_model_ids: [],
  class: 'interactive',
  temperature: null,
  max_output: 0,
  ttft_ms: 20000,
  idle_ms: 30000,
  total_ms: 120000
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
