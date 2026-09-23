export const SHOP_KIND_LABEL: Record<ShopKind, string> = {
  avatar_frame: '头像框',
  profile_background: '主页背景'
}

export const SHOP_SLOTS: {
  slot: ShopKind
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
