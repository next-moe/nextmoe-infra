export const ROLE_COLOR: Record<
  string,
  'primary' | 'success' | 'warning' | 'secondary' | 'default'
> = {
  ren: 'secondary',
  admin: 'primary',
  moderator: 'warning',
  creator: 'success',
  editor: 'default',
  user: 'default',
}

export const roleColor = (role: string) => ROLE_COLOR[role] ?? 'default'

export const ROLE_LABEL: Record<string, string> = {
  ren: '莲',
  admin: '管理员',
  moderator: '版主',
  creator: '创作者',
  editor: '编辑者',
  user: '用户',
}

export const roleLabel = (role: string) => ROLE_LABEL[role] ?? role

const ROLE_PRIORITY = ['ren', 'admin', 'moderator', 'creator', 'editor']
export const primaryRole = (roles: string[] = []) =>
  ROLE_PRIORITY.find((r) => roles.includes(r)) ?? ''

export const STEP_UP_ROLES = ['admin', 'ren']
export const needsStepUp = (roles: string[] = []) =>
  roles.some((r) => STEP_UP_ROLES.includes(r))

export const USER_ROLE_FILTER_ALL = 'all'
export const USER_ROLE_FILTER_OPTIONS = [
  { value: USER_ROLE_FILTER_ALL, label: '全部角色' },
  { value: 'user', label: '普通用户' },
  ...['creator', 'editor', 'moderator', 'admin', 'ren'].map((r) => ({
    value: r,
    label: ROLE_LABEL[r]!,
  })),
]
