export type TelemetryChipColor =
  | 'default'
  | 'primary'
  | 'secondary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'

export const TELEMETRY_ISSUE_KINDS = [
  'exception',
  'contract',
  'server',
  'java',
  'native',
  'anr'
] as const

export type TelemetryIssueKind = (typeof TELEMETRY_ISSUE_KINDS)[number]

export const TELEMETRY_KIND_META: Record<
  TelemetryIssueKind,
  { label: string; color: TelemetryChipColor }
> = {
  exception: { label: '异常', color: 'warning' },
  contract: { label: '契约不一致', color: 'warning' },
  server: { label: '服务端 5xx', color: 'danger' },
  java: { label: 'Java 崩溃', color: 'danger' },
  native: { label: '原生崩溃', color: 'danger' },
  anr: { label: 'ANR', color: 'warning' }
}

export const TELEMETRY_ISSUE_STATUSES = ['open', 'resolved', 'ignored'] as const

export type TelemetryIssueStatus = (typeof TELEMETRY_ISSUE_STATUSES)[number]

export const TELEMETRY_ISSUE_STATUS_META: Record<
  TelemetryIssueStatus,
  { label: string; color: TelemetryChipColor }
> = {
  open: { label: '未解决', color: 'warning' },
  resolved: { label: '已解决', color: 'success' },
  ignored: { label: '已忽略', color: 'default' }
}

export const TELEMETRY_CRASH_STATUSES = [
  'pending',
  'done',
  'waiting_symbols',
  'failed'
] as const

export type TelemetryCrashStatus = (typeof TELEMETRY_CRASH_STATUSES)[number]

export const TELEMETRY_CRASH_STATUS_META: Record<
  TelemetryCrashStatus,
  { label: string; color: TelemetryChipColor }
> = {
  pending: { label: '待处理', color: 'default' },
  done: { label: '已还原', color: 'success' },
  waiting_symbols: { label: '等待符号', color: 'warning' },
  failed: { label: '还原失败', color: 'danger' }
}

export const TELEMETRY_ALERT_RULES = [
  'contract_new',
  'issue_new',
  'issue_regressed',
  'crash_rate',
  'anr_rate',
  'crash_regression',
  'server_faults',
  'symbols_missing',
  'silent_app',
  'engine_fetch_failed'
] as const

export type TelemetryAlertRule = (typeof TELEMETRY_ALERT_RULES)[number]

export const TELEMETRY_ALERT_RULE_LABELS: Record<TelemetryAlertRule, string> = {
  contract_new: '契约不一致',
  issue_new: '新问题',
  issue_regressed: '问题回归',
  crash_rate: '崩溃率超标',
  anr_rate: 'ANR 率超标',
  crash_regression: '崩溃率较上一版本变差',
  server_faults: '服务端 5xx 过多',
  symbols_missing: '符号缺失',
  silent_app: 'App 无上报',
  engine_fetch_failed: '引擎符号拉取失败'
}

export const TELEMETRY_ALERT_STATUSES = [
  'queued',
  'sent',
  'failed',
  'suppressed'
] as const

export type TelemetryAlertStatus = (typeof TELEMETRY_ALERT_STATUSES)[number]

export const TELEMETRY_ALERT_STATUS_META: Record<
  TelemetryAlertStatus,
  { label: string; color: TelemetryChipColor }
> = {
  queued: { label: '排队中', color: 'default' },
  sent: { label: '已发送', color: 'success' },
  failed: { label: '发送失败', color: 'danger' },
  suppressed: { label: '未发送', color: 'default' }
}

export const TELEMETRY_ENVIRONMENTS = ['direct', 'dev', 'check'] as const

export type TelemetryEnvironment = (typeof TELEMETRY_ENVIRONMENTS)[number]

export const TELEMETRY_ENVIRONMENT_LABELS: Record<
  TelemetryEnvironment,
  string
> = {
  direct: '正式渠道',
  dev: '开发',
  check: '本机检查'
}

export const TELEMETRY_WINDOW_OPTIONS = [
  { value: 7, label: '近 7 天' },
  { value: 14, label: '近 14 天' },
  { value: 30, label: '近 30 天' },
  { value: 90, label: '近 90 天' }
] as const

export type TelemetryWindowDays =
  (typeof TELEMETRY_WINDOW_OPTIONS)[number]['value']

export const TELEMETRY_DEFAULT_WINDOW: TelemetryWindowDays = 14

export const TELEMETRY_ISSUE_SORT_OPTIONS = [
  { value: 'events', label: '事件数' },
  { value: 'last_seen', label: '最近出现' }
] as const

export const TELEMETRY_ENGINE_STATUS_META: Record<
  string,
  { label: string; color: TelemetryChipColor }
> = {
  pending: { label: '拉取中', color: 'warning' },
  ready: { label: '已就绪', color: 'success' },
  failed: { label: '失败', color: 'danger' }
}

export const TELEMETRY_SYMBOL_KIND_LABELS: Record<string, string> = {
  dart_symbols: 'Dart 符号',
  dart_obfuscation_map: 'Dart 混淆映射',
  r8_mapping: 'R8 映射'
}

export const TELEMETRY_SERVICE_NAME_RE = /^[a-z][a-z0-9-]{1,62}$/

export const TELEMETRY_ALERT_DEFAULTS = {
  crash_rate: 0.0109,
  anr_rate: 0.0047,
  min_sessions: 200,
  regression_factor: 2,
  regression_min_delta: 0.005,
  server_faults_per_hour: 50,
  silent_hours: 6,
  silent_min_daily_sessions: 50
} as const

export const TELEMETRY_ALERT_SETTING_FIELDS = [
  {
    key: 'crash_rate',
    label: '崩溃率阈值',
    hint: '某版本崩溃率超过该值时告警',
    kind: 'percent' as const,
    placeholder: '1.09',
    min: 0.01,
    max: 99.99,
    step: 0.01
  },
  {
    key: 'anr_rate',
    label: 'ANR 率阈值',
    hint: '某版本 ANR 率超过该值时告警',
    kind: 'percent' as const,
    placeholder: '0.47',
    min: 0.01,
    max: 99.99,
    step: 0.01
  },
  {
    key: 'min_sessions',
    label: '最少会话数',
    hint: '会话数少于此值不按比率告警',
    kind: 'int' as const,
    placeholder: '200',
    min: 1,
    max: 1_000_000,
    step: 1
  },
  {
    key: 'regression_factor',
    label: '崩溃率回归倍数',
    hint: '相对上一版本崩溃率的倍数',
    kind: 'number' as const,
    placeholder: '2',
    min: 1,
    max: 100,
    step: 0.1
  },
  {
    key: 'regression_min_delta',
    label: '回归最小差值',
    hint: '崩溃率至少再升高这么多才算变差',
    kind: 'percent' as const,
    placeholder: '0.5',
    min: 0,
    max: 100,
    step: 0.01
  },
  {
    key: 'server_faults_per_hour',
    label: '每小时服务端 5xx',
    hint: '一小时内服务端 5xx 超过该条数时告警',
    kind: 'int' as const,
    placeholder: '50',
    min: 1,
    max: 1_000_000,
    step: 1
  },
  {
    key: 'silent_hours',
    label: '无上报小时数',
    hint: '平时有流量的应用连续无上报这么久后告警',
    kind: 'int' as const,
    placeholder: '6',
    min: 1,
    max: 168,
    step: 1
  },
  {
    key: 'silent_min_daily_sessions',
    label: '静默判定日会话下限',
    hint: '日会话达到该值才视为「平时有流量」',
    kind: 'int' as const,
    placeholder: '50',
    min: 1,
    max: 1_000_000,
    step: 1
  }
] as const

export type TelemetryAlertSettingKey =
  (typeof TELEMETRY_ALERT_SETTING_FIELDS)[number]['key']

export const telemetryUtcDay = (d: Date) => {
  const y = d.getUTCFullYear()
  const m = String(d.getUTCMonth() + 1).padStart(2, '0')
  const day = String(d.getUTCDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

export const telemetryWindowRange = (days: number) => {
  const now = new Date()
  const to = new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  )
  const from = new Date(to)
  from.setUTCDate(from.getUTCDate() - (days - 1))
  return { from: telemetryUtcDay(from), to: telemetryUtcDay(to) }
}

export const telemetryDayList = (from: string, to: string) => {
  const out: string[] = []
  const fp = from.split('-').map(Number)
  const tp = to.split('-').map(Number)
  const fy = fp[0] ?? 0
  const fm = fp[1] ?? 1
  const fd = fp[2] ?? 1
  const ty = tp[0] ?? 0
  const tm = tp[1] ?? 1
  const td = tp[2] ?? 1
  const cur = new Date(Date.UTC(fy, fm - 1, fd))
  const end = new Date(Date.UTC(ty, tm - 1, td))
  while (cur.getTime() <= end.getTime()) {
    out.push(telemetryUtcDay(cur))
    cur.setUTCDate(cur.getUTCDate() + 1)
  }
  return out
}

export const formatTelemetryRate = (
  rate: number | null | undefined,
  sessions: number
) => {
  if (sessions <= 0 || rate == null || !Number.isFinite(rate)) return '—'
  return `${(rate * 100).toFixed(2)}%`
}

export const formatTelemetryMs = (ms: number | null | undefined) => {
  if (ms == null || !Number.isFinite(ms)) return '—'
  return `${Math.round(ms)} ms`
}

export const formatTelemetryNeeds = (needs: string) => {
  const parts = needs
    .trim()
    .split(/\s+/)
    .filter((p) => p.length > 0)
  if (!parts.length) return ''
  return parts
    .map((p) => {
      const i = p.indexOf(':')
      if (i <= 0) return p
      const kind = p.slice(0, i)
      const rest = p.slice(i + 1)
      if (kind === 'dart') return `缺少 Dart 符号 ${rest}`
      if (kind === 'r8') return `缺少 R8 映射 ${rest}`
      if (kind === 'engine') return `缺少引擎符号 ${rest}`
      return p
    })
    .join('；')
}

export const telemetryKindMeta = (kind: string) =>
  TELEMETRY_KIND_META[kind as TelemetryIssueKind] ?? {
    label: kind,
    color: 'default' as const
  }

export const telemetryIssueStatusMeta = (status: string) =>
  TELEMETRY_ISSUE_STATUS_META[status as TelemetryIssueStatus] ?? {
    label: status,
    color: 'default' as const
  }

export const telemetryCrashStatusMeta = (status: string) =>
  TELEMETRY_CRASH_STATUS_META[status as TelemetryCrashStatus] ?? {
    label: status,
    color: 'default' as const
  }

export const telemetryAlertStatusMeta = (status: string) =>
  TELEMETRY_ALERT_STATUS_META[status as TelemetryAlertStatus] ?? {
    label: status,
    color: 'default' as const
  }

export const telemetryAlertRuleLabel = (rule: string) =>
  TELEMETRY_ALERT_RULE_LABELS[rule as TelemetryAlertRule] ?? rule

export const telemetryPageCount = (page: number, pageSize: number, n: number) =>
  Math.max(1, page + (n >= pageSize ? 1 : 0))
