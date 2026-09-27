export const SHOP_KIND_ICON: Partial<Record<ShopKind, string>> = {
  profile_about: 'lucide:notebook-pen',
  redeem_code: 'lucide:ticket-percent'
}

export const SHOP_STAGE_TINT: Record<ShopKind, string> = {
  avatar_frame: 'bg-secondary-50',
  profile_background: 'bg-default-100',
  profile_about: 'bg-primary-50',
  redeem_code: 'bg-warning-50'
}

export interface ShopShelf {
  key: string
  title: string
  note: string
  icon: string
  tint: string
}

export const SHOP_SHELVES: ShopShelf[] = [
  {
    key: 'codes',
    title: '福利兑换',
    note: '兑换码买到后立即发放，在「我的物品」里随时可以找到',
    icon: 'lucide:ticket-percent',
    tint: 'bg-warning-100 text-warning-600'
  },
  {
    key: 'perks',
    title: '功能解锁',
    note: '给你的账号多一项能力',
    icon: 'lucide:sparkles',
    tint: 'bg-primary-100 text-primary-600'
  },
  {
    key: 'cosmetics',
    title: '装扮',
    note: '换上之后，所有站点都会显示',
    icon: 'lucide:shirt',
    tint: 'bg-secondary-100 text-secondary-600'
  }
]

export const SHOP_KIND_SHELF: Record<ShopKind, string> = {
  redeem_code: 'codes',
  profile_about: 'perks',
  avatar_frame: 'cosmetics',
  profile_background: 'cosmetics'
}

export const SHOP_SITE_SHELF_TINT = 'bg-info-100 text-info-600'

export const SHOP_SLOTS: {
  slot: ShopSlot
  label: string
  empty: string
  worn: string
}[] = [
  {
    slot: 'avatar_frame',
    label: '头像框',
    empty: '你还没有头像框',
    worn: '摘下头像框'
  },
  {
    slot: 'profile_background',
    label: '主页背景',
    empty: '你还没有主页背景',
    worn: '取下主页背景'
  }
]

export const PROFILE_ABOUT_MAX = 500
