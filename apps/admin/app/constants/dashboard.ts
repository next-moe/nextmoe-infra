export const DASHBOARD_STAT_CARDS = [
  { key: 'users', label: '用户总数', icon: 'lucide:users', color: 'bg-primary' },
  { key: 'sites', label: '站点数量', icon: 'lucide:globe', color: 'bg-default' },
  { key: 'clients', label: 'OAuth 客户端', icon: 'lucide:key', color: 'bg-warning' },
] as const

export const QUICK_ACTIONS = [
  { label: '用户管理', to: '/users', icon: 'lucide:user-plus', color: 'text-primary' },
  { label: '站点设置', to: '/sites', icon: 'lucide:settings', color: 'text-default' },
  { label: 'OAuth 客户端', to: '/oauth-clients', icon: 'lucide:key', color: 'text-warning' },
  { label: 'T&S 审核', to: '/trust', icon: 'lucide:shield', color: 'text-success' },
]
