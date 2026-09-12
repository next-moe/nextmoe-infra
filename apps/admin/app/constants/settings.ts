import type {
  SettingKind,
  SettingSource,
  SettingValue,
  SettingsAuditAction
} from '~~/shared/types/settings'

export type SettingsChipColor =
  | 'default'
  | 'primary'
  | 'secondary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'

export const KIND_LABELS: Record<SettingKind, string> = {
  bool: '布尔',
  int: '整数',
  float: '小数',
  string: '文本',
  enum: '枚举',
  string_list: '列表'
}

export const SOURCE_LABELS: Record<SettingSource, string> = {
  db: '已覆盖',
  default: '默认',
  site: '站点覆盖'
}

export const SOURCE_COLORS: Record<SettingSource, SettingsChipColor> = {
  db: 'primary',
  default: 'default',
  site: 'secondary'
}

export const AUDIT_ACTION_LABELS: Record<SettingsAuditAction, string> = {
  set: '设置',
  reset: '撤销覆盖'
}

export const AUDIT_ACTION_COLORS: Record<
  SettingsAuditAction,
  SettingsChipColor
> = {
  set: 'success',
  reset: 'warning'
}

export const AUDIT_LIST_LIMIT = 20

export const PROPAGATION_NOTE = '保存后所有服务在 30 秒内生效'

export const ENV_FLOOR_NOTE =
  '若所属服务的环境里设置了该变量,它会覆盖默认值,直到这里设置了覆盖值'

export const PLATFORM_SCOPE_LABEL = '平台(全局)'

export const PUBLIC_BADGE = '公开'

export const PUBLIC_NOTE = '通过 GET /api/v1/settings 下发给各站点(S2S 读面)'

export const SITE_SCOPED_BADGE = '可按站点'

export const SITE_SCOPED_NOTE = '选中某个站点后可单独覆盖'

export const SITE_SCOPE_HINT =
  '仅列出可按站点覆盖的键;没有站点覆盖值的键显示平台生效值'

export const formatSettingValue = (
  kind: SettingKind,
  v: SettingValue | null | undefined
): string => {
  if (v === null || v === undefined) {
    return '—'
  }
  if (kind === 'bool') {
    return v === true ? '开' : '关'
  }
  if (kind === 'string_list') {
    if (!Array.isArray(v) || v.length === 0) {
      return '(空)'
    }
    return v.join(', ')
  }
  if (typeof v === 'string') {
    return v === '' ? '(空)' : v
  }
  return String(v)
}
