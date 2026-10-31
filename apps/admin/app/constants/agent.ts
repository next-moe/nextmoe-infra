import type { AiChipColor } from '~/constants/ai'
import type {
  AgentSiteToolAuth,
  AgentSiteToolForm
} from '~~/shared/types/agent'

export const AGENT_API = '/admin/agent'

export const AGENT_TOOL_AUTH_META: Record<
  AgentSiteToolAuth,
  { label: string; color: AiChipColor; hint: string }
> = {
  user_token: {
    label: '转发用户令牌',
    color: 'primary',
    hint: '把提问用户的令牌转给站点,由站点按自己的可见性规则返回;未登录的搜索会跳过这些工具'
  },
  none: {
    label: '不鉴权',
    color: 'default',
    hint: '匿名调用,只能返回公开内容'
  }
}

export const AGENT_TOOL_AUTH_OPTIONS = (
  Object.keys(AGENT_TOOL_AUTH_META) as AgentSiteToolAuth[]
).map((value) => ({ value, label: AGENT_TOOL_AUTH_META[value].label }))

export const AGENT_TOOL_NAME_PATTERN = /^[a-z][a-z0-9_]{0,31}$/

export const AGENT_TOOL_DEFAULTS: AgentSiteToolForm = {
  site_id: 0,
  name: '',
  url: '',
  auth: 'user_token',
  card_prefix: '',
  allowed: [],
  timeout_ms: 20000,
  enabled: true
}

export const AGENT_TOOL_TIMEOUT_RANGE = { min: 1000, max: 120000 } as const

export const AGENT_DEFAULT_SITE = 0

export const AGENT_PERSONA_PROMPT_HINT =
  '写她是谁、怎么说话、熟悉什么。工具使用、注明来源、附件安全等通用规则追加在人设后面，在设置中心「AI 助手」的 agent.prompt.* 里修改。'

export const AGENT_PERSONA_PLACEHOLDER =
  '你是「小萌」，NextMoe 的看板娘，活泼、友善，熟悉 galgame、同人游戏和 ACG。用简洁自然的中文回答；不知道的事情直说不知道。'
