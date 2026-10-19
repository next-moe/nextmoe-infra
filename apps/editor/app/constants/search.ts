export interface SearchFamily {
  object: 'work' | 'character' | 'credit_name' | 'company'
  label: string
  icon: string
  path: string
}

export const SEARCH_FAMILIES: SearchFamily[] = [
  { object: 'work', label: '作品', icon: 'lucide:gamepad-2', path: '/works' },
  {
    object: 'character',
    label: '角色',
    icon: 'lucide:user-round',
    path: '/characters'
  },
  {
    object: 'credit_name',
    label: '署名名',
    icon: 'lucide:signature',
    path: '/credit-names'
  },
  {
    object: 'company',
    label: '厂商',
    icon: 'lucide:building-2',
    path: '/companies'
  }
]
