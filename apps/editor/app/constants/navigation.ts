export interface EditorNavItem {
  label: string
  icon: string
  to: string
  ready?: boolean
  reviewerOnly?: boolean
}

export interface EditorNavGroup {
  label?: string
  items: EditorNavItem[]
}

export const EDITOR_NAV: EditorNavGroup[] = [
  {
    items: [
      { label: '工作台', icon: 'lucide:layout-dashboard', to: '/', ready: true }
    ]
  },
  {
    label: '条目',
    items: [
      { label: '作品', icon: 'lucide:gamepad-2', to: '/works', ready: true },
      { label: '发行', icon: 'lucide:package', to: '/releases', ready: true },
      {
        label: '角色',
        icon: 'lucide:user-round',
        to: '/characters',
        ready: true
      },
      { label: '人物', icon: 'lucide:contact', to: '/persons', ready: true },
      {
        label: '署名名',
        icon: 'lucide:signature',
        to: '/credit-names',
        ready: true
      },
      {
        label: '厂商',
        icon: 'lucide:building-2',
        to: '/companies',
        ready: true
      },
      { label: '标签', icon: 'lucide:tags', to: '/tags', ready: true },
      { label: '特质', icon: 'lucide:sparkles', to: '/traits', ready: true },
      { label: '系列', icon: 'lucide:library', to: '/series', ready: true },
      { label: '引擎', icon: 'lucide:cpu', to: '/engines', ready: true }
    ]
  },
  {
    label: '我的',
    items: [
      {
        label: '提案',
        icon: 'lucide:git-pull-request',
        to: '/me/proposals',
        ready: true
      },
      {
        label: '草稿',
        icon: 'lucide:file-pen-line',
        to: '/me/drafts',
        ready: true
      },
      {
        label: '合并请求',
        icon: 'lucide:merge',
        to: '/me/merge-requests',
        ready: true
      },
      {
        label: '删除请求',
        icon: 'lucide:trash',
        to: '/me/deletion-requests',
        ready: true
      },
      {
        label: '贡献',
        icon: 'lucide:award',
        to: '/me/contributions',
        ready: true
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
        ready: true,
        reviewerOnly: true
      },
      {
        label: '请求审核',
        icon: 'lucide:git-merge',
        to: '/review/requests',
        ready: true,
        reviewerOnly: true
      },
    ]
  },
  {
    items: [
      {
        label: '最近修订',
        icon: 'lucide:history',
        to: '/changes',
        ready: true
      },
      {
        label: '编辑规范',
        icon: 'lucide:book-open',
        to: '/guidelines',
        ready: true
      }
    ]
  }
]

export const findNavItem = (path: string) =>
  EDITOR_NAV.flatMap((group) => group.items).find(
    (item) =>
      item.to !== '/' && (path === item.to || path.startsWith(`${item.to}/`))
  )

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
