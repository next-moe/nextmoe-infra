export interface SidebarItem {
  icon: string
  label: string
  to?: string
  adminOnly?: boolean
  renOnly?: boolean
  children?: SidebarItem[]
}

export const SIDEBAR_MENU: SidebarItem[] = [
  { icon: 'lucide:layout-dashboard', label: '仪表盘', to: '/', adminOnly: true },
  { icon: 'lucide:users', label: '用户管理', to: '/users', adminOnly: true },
  { icon: 'lucide:globe', label: '站点管理', to: '/sites', adminOnly: true },
  { icon: 'lucide:key', label: 'OAuth 客户端', to: '/oauth-clients', adminOnly: true },
  { icon: 'lucide:shield', label: 'T&S 审核', to: '/trust', adminOnly: true },
  { icon: 'lucide:newspaper', label: '情报审核', to: '/news', adminOnly: true },
  { icon: 'lucide:user-plus', label: '创作者申请', to: '/creator-applications', adminOnly: true },
  { icon: 'lucide:image', label: '图片管理', to: '/images', adminOnly: true },
  { icon: 'lucide:activity', label: '后台任务', to: '/jobs', adminOnly: true },
  {
    icon: 'lucide:package',
    label: '文件存储',
    adminOnly: true,
    children: [
      { icon: 'lucide:chart-pie', label: '用量概览', to: '/artifacts', adminOnly: true },
      { icon: 'lucide:files', label: '文件列表', to: '/artifacts/list', renOnly: true },
      { icon: 'lucide:sliders-horizontal', label: '存储配置', to: '/artifacts/config', renOnly: true },
    ],
  },
  {
    icon: 'lucide:library',
    label: '目录人审',
    adminOnly: true,
    children: [
      { icon: 'lucide:git-compare', label: '候选', to: '/catalog/candidates', adminOnly: true },
      { icon: 'lucide:git-merge', label: '合并提案', to: '/catalog/proposals', adminOnly: true },
      { icon: 'lucide:link', label: '外链确认', to: '/catalog/refs', adminOnly: true },
    ],
  },
  {
    icon: 'lucide:terminal',
    label: '开发者平台',
    adminOnly: true,
    children: [
      { icon: 'lucide:layout-grid', label: '应用与策略', to: '/devapi', adminOnly: true },
      { icon: 'lucide:key-round', label: '全部密钥', to: '/devapi/keys', adminOnly: true },
    ],
  },
  { icon: 'lucide:shield-check', label: '权限矩阵', to: '/permission', adminOnly: true },
  { icon: 'lucide:settings-2', label: '配置中心', to: '/settings', adminOnly: true },
  { icon: 'lucide:user', label: '个人信息', to: '/profile' },
]

export const IMAGE_REVIEW_STATUS_MAP: Record<string, { label: string; color: 'warning' | 'success' | 'danger' | 'default' }> = {
  pending: { label: '待审核', color: 'warning' },
  approved: { label: '已通过', color: 'success' },
  rejected: { label: '已拒绝', color: 'danger' },
  manual_review: { label: '人工复核', color: 'default' },
  unknown: { label: '未知', color: 'default' },
}

export const IMAGE_VARIANT_DIMENSIONS: Record<string, { w: number; h: number }> = {
  mini: { w: 460, h: 259 },
  '256': { w: 256, h: 256 },
  '100': { w: 100, h: 100 },
}

export const IMAGE_STATUS_TABS = [
  { id: '', label: '全部', icon: 'lucide:list' },
  { id: 'pending', label: '待审核', icon: 'lucide:clock' },
  { id: 'manual_review', label: '人工复核', icon: 'lucide:user-check' },
  { id: 'rejected', label: '已拒绝', icon: 'lucide:x' },
  { id: 'approved', label: '已通过', icon: 'lucide:check' },
]

export const USER_STATUS_MAP: Record<number, { label: string; color: 'success' | 'danger' | 'default' }> = {
  0: { label: '正常', color: 'success' },
  1: { label: '已封禁', color: 'danger' },
}
