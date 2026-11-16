import type { KunTabItem } from '@kungal/ui-vue'
import type { SettingsChipColor } from '~/constants/settings'
import type {
  EnvAuditAction,
  EnvIssue,
  EnvSource,
  EnvVarKind,
  EnvVarView
} from '~~/shared/types/settings'

export const SETTINGS_NAV_TABS: KunTabItem[] = [
  { value: '/settings', textValue: '运行期配置' },
  { value: '/settings/env', textValue: '环境变量与密钥' },
  { value: '/settings/audit', textValue: '变更记录' }
]

export const ENV_SOURCE_LABELS: Record<EnvSource, string> = {
  env: '环境变量',
  store: '配置中心',
  default: '代码默认'
}

export const ENV_SOURCE_COLORS: Record<EnvSource, SettingsChipColor> = {
  env: 'warning',
  store: 'primary',
  default: 'default'
}

export const ENV_KIND_LABELS: Record<EnvVarKind, string> = {
  bootstrap: '启动接线',
  topology: '拓扑',
  panel: '第三方共用',
  storable: ''
}

export const ENV_KIND_NOTES: Record<EnvVarKind, string> = {
  bootstrap:
    '读配置中心之前就要用到(数据库连接与主密钥),只能留在 compose / Dokploy',
  topology: '服务监听与互相寻址的方式,随部署一起在 compose 里评审',
  panel: 'centrifugo 容器也从 Dokploy 读这个值,只能留在面板',
  storable: ''
}

export const ENV_ISSUE_LABELS: Record<EnvIssue, string> = {
  env_redundant: '面板值可删除',
  env_shadows: '面板值覆盖了配置中心',
  restart_pending: '待重启生效',
  kek_missing: '服务缺少主密钥'
}

export const ENV_ISSUE_COLORS: Record<EnvIssue, SettingsChipColor> = {
  env_redundant: 'success',
  env_shadows: 'danger',
  restart_pending: 'warning',
  kek_missing: 'danger'
}

export const ENV_ISSUE_NOTES: Record<EnvIssue, string> = {
  env_redundant:
    '环境变量与配置中心的值一致;从 Dokploy 删掉它,下次部署后来源会变成「配置中心」',
  env_shadows:
    '环境变量与配置中心的值不同,环境变量优先;确认哪个是对的,再删面板值或改存储值',
  restart_pending: '存储值在该服务启动之后改过,重启该服务才会读到',
  kek_missing: '该服务启动时没有 KUN_CONFIG_KEK,读不到配置中心'
}

export const ENV_AUDIT_ACTION_LABELS: Record<EnvAuditAction, string> = {
  set: '写入',
  reset: '撤销',
  import: '导入'
}

export const ENV_AUDIT_ACTION_COLORS: Record<
  EnvAuditAction,
  SettingsChipColor
> = {
  set: 'success',
  reset: 'warning',
  import: 'info'
}

export const ENV_FILTERS = [
  { value: 'all', label: '全部' },
  { value: 'issues', label: '有问题' },
  { value: 'secret', label: '密钥' },
  { value: 'stored', label: '已存入配置中心' },
  { value: 'env', label: '来自环境变量' },
  { value: 'default', label: '全部为代码默认' }
] as const

export type EnvFilter = (typeof ENV_FILTERS)[number]['value']

export const matchesEnvFilter = (row: EnvVarView, filter: EnvFilter) => {
  switch (filter) {
    case 'issues':
      return row.issues.length > 0
    case 'secret':
      return row.secret
    case 'stored':
      return row.stored.length > 0
    case 'env':
      return row.services.some((s) => s.source === 'env')
    case 'default':
      return row.services.every((s) => s.source === 'default')
    default:
      return true
  }
}

const ENV_GROUP_RULES: { value: string; label: string; prefixes: string[] }[] =
  [
    { value: 'database', label: '数据库', prefixes: ['KUN_PG_'] },
    { value: 'redis', label: 'Redis', prefixes: ['REDIS_'] },
    {
      value: 'auth',
      label: '账号与认证',
      prefixes: [
        'JWT_',
        'KUN_OIDC_',
        'KUN_FEDERATION_',
        'KUN_SITE_URL',
        'KUN_FRONTEND_',
        'KUN_FIBER_',
        'KUN_DEV_PORTAL_',
        'KUN_VISUAL_NOVEL_EMAIL_',
        'KUN_ENV',
        'KUN_EDITOR_'
      ]
    },
    {
      value: 'image',
      label: '图床',
      prefixes: ['KUN_IMAGE', 'KUN_GALGAME_IMAGE_']
    },
    { value: 'artifact', label: '文件存储', prefixes: ['KUN_ARTIFACT'] },
    {
      value: 'catalog',
      label: '目录与商店',
      prefixes: [
        'KUN_CATALOG_',
        'KUN_STORE_',
        'KUN_YMGAL_',
        'KUN_OPENSEARCH_',
        'KUN_NEWS_'
      ]
    },
    {
      value: 'community',
      label: '社区与聊天',
      prefixes: ['KUN_COMMUNITY_', 'KUN_CHAT_']
    },
    { value: 'trust', label: 'Trust & Safety', prefixes: ['KUN_TRUST_'] },
    {
      value: 'ai',
      label: 'AI',
      prefixes: ['KUN_AI_', 'KUN_LLM_', 'KUN_AGENT_']
    },
    { value: 'telemetry', label: '遥测', prefixes: ['KUN_TELEMETRY_'] }
  ]

export const envGroupOf = (key: string): string => {
  if (key.includes('_PG_')) return 'database'
  return (
    ENV_GROUP_RULES.find((g) => g.prefixes.some((p) => key.startsWith(p)))
      ?.value ?? 'other'
  )
}

export const ENV_GROUP_LABELS: Record<string, string> = {
  ...Object.fromEntries(ENV_GROUP_RULES.map((g) => [g.value, g.label])),
  other: '其他'
}

export const ENV_GROUP_ORDER = [...ENV_GROUP_RULES.map((g) => g.value), 'other']

export const ENV_SCOPE_ALL_LABEL = '全部服务'

export const envScopeLabel = (scope: string) =>
  scope === '' ? ENV_SCOPE_ALL_LABEL : scope

export const ENV_PRECEDENCE_NOTE =
  '生效顺序:环境变量 → 配置中心 → 代码默认。配置中心的值在服务启动时读取,改动后需重启对应服务;密钥只显示指纹与末尾几位。'
