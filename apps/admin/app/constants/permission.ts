import type {
  PermissionAuditAction,
  PermissionCell
} from '~~/shared/types/permission'


export type PermissionChipColor =
  | 'default'
  | 'primary'
  | 'secondary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'

export const AUDIT_ACTION_LABELS: Record<PermissionAuditAction, string> = {
  grant: '授予',
  revoke: '撤销授予',
  deny: '撤销权限',
  revoke_deny: '恢复权限'
}

export const AUDIT_ACTION_COLORS: Record<
  PermissionAuditAction,
  PermissionChipColor
> = {
  grant: 'success',
  revoke: 'warning',
  deny: 'danger',
  revoke_deny: 'info'
}

export type PermissionCellOp = 'grant' | 'revoke' | 'deny' | 'restore'

export const CELL_OP_LABELS: Record<PermissionCellOp, string> = {
  grant: '授予',
  revoke: '撤销叠加授权',
  deny: '撤销（记 deny）',
  restore: '恢复'
}

export const cellOp = (cell: PermissionCell): PermissionCellOp | null => {
  if (cell.editable) return cell.source === 'grant' ? 'revoke' : 'grant'
  if (cell.can_deny) return 'deny'
  if (cell.can_restore) return 'restore'
  return null
}

export const BLAST_RADIUS_NOTE = '作用于所有持有该角色的用户，≤30 秒生效'

export const AUDIT_LIST_LIMIT = 20

export const IMMUTABLE_ROLE_NOTES: Record<string, string> = {
  user: '普通用户的 JWT roles 为空数组，授予它的权限永远不会生效',
  ren: 'ren 既是包含性不变量的上界，也是锁死后的恢复保险：任何叠加行都不能削减它，只能改代码捆并部署'
}
