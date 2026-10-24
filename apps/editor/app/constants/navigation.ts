export interface EditorNavItem {
  label: string
  icon: string
  to: string
  reviewerOnly?: boolean
}

export interface EditorNavGroup {
  label?: string
  items: EditorNavItem[]
}

export const EDITOR_NAV: EditorNavGroup[] = [
  {
    items: [{ label: '工作台', icon: 'lucide:layout-dashboard', to: '/' }]
  },
  {
    label: '条目',
    items: [
      { label: '作品', icon: 'lucide:gamepad-2', to: '/works' },
      { label: '发行', icon: 'lucide:package', to: '/releases' },
      {
        label: '角色',
        icon: 'lucide:user-round',
        to: '/characters'
      },
      { label: '人物', icon: 'lucide:contact', to: '/persons' },
      {
        label: '署名名',
        icon: 'lucide:signature',
        to: '/credit-names'
      },
      {
        label: '厂商',
        icon: 'lucide:building-2',
        to: '/companies'
      },
      { label: '标签', icon: 'lucide:tags', to: '/tags' },
      { label: '特质', icon: 'lucide:sparkles', to: '/traits' },
      { label: '系列', icon: 'lucide:library', to: '/series' },
      { label: '引擎', icon: 'lucide:cpu', to: '/engines' }
    ]
  },
  {
    label: '我的',
    items: [
      {
        label: '提案',
        icon: 'lucide:git-pull-request',
        to: '/me/proposals'
      },
      {
        label: '草稿',
        icon: 'lucide:file-pen-line',
        to: '/me/drafts'
      },
      {
        label: '合并请求',
        icon: 'lucide:merge',
        to: '/me/merge-requests'
      },
      {
        label: '拆分请求',
        icon: 'lucide:split',
        to: '/me/split-requests'
      },
      {
        label: '删除请求',
        icon: 'lucide:trash',
        to: '/me/deletion-requests'
      },
      {
        label: '贡献',
        icon: 'lucide:award',
        to: '/me/contributions'
      }
    ]
  },
  {
    label: '审核',
    items: [
      {
        label: '待审队列',
        icon: 'lucide:inbox',
        to: '/review',
        reviewerOnly: true
      },
      {
        label: '请求审核',
        icon: 'lucide:git-merge',
        to: '/review/requests',
        reviewerOnly: true
      }
    ]
  },
  {
    items: [
      {
        label: '最近修订',
        icon: 'lucide:history',
        to: '/changes'
      },
      {
        label: '编辑规范',
        icon: 'lucide:book-open',
        to: '/guidelines'
      },
      {
        label: '设置',
        icon: 'lucide:settings',
        to: '/settings'
      }
    ]
  }
]

export const currentNavPath = (path: string, items: EditorNavItem[]) =>
  items
    .filter((item) =>
      item.to === '/'
        ? path === '/'
        : path === item.to || path.startsWith(`${item.to}/`)
    )
    .reduce<string | null>(
      (best, item) =>
        best === null || item.to.length > best.length ? item.to : best,
      null
    )
