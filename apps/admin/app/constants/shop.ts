import type { KunUIColor } from '@kungal/ui-core'
import type {
  ShopItemStatus,
  ShopKind,
  ShopOfferStatus
} from '~~/shared/types/shop'

export const SHOP_ITEM_STATUS: Record<
  ShopItemStatus,
  { label: string; color: KunUIColor }
> = {
  draft: { label: '草稿', color: 'default' },
  review: { label: '待审核', color: 'warning' },
  published: { label: '已发布', color: 'success' },
  retired: { label: '已下架', color: 'danger' }
}

export const SHOP_ITEM_ACTIONS: Record<
  ShopItemStatus,
  { action: string; label: string }[]
> = {
  draft: [
    { action: 'submit', label: '提交审核' },
    { action: 'publish', label: '发布' }
  ],
  review: [
    { action: 'publish', label: '发布' },
    { action: 'reject', label: '驳回' }
  ],
  published: [{ action: 'retire', label: '下架' }],
  retired: [{ action: 'relist', label: '重新发布' }]
}

export const SHOP_OFFER_STATUS: Record<
  ShopOfferStatus,
  { label: string; color: KunUIColor }
> = {
  draft: { label: '未上架', color: 'default' },
  active: { label: '在售', color: 'success' },
  retired: { label: '已下架', color: 'danger' }
}

export const SHOP_KINDS: Record<
  ShopKind,
  { label: string; hint: string; staticAccept: string }
> = {
  avatar_frame: {
    label: '头像框',
    hint: '画布是头像的 1.2 倍的正方形，头像圆居中；四角和中心必须透明。静态图用 PNG（≤ 512 KB），动图用带透明通道的动态 WebP（≤ 2 MB，和静态图同尺寸）。',
    staticAccept: 'image/png'
  },
  profile_background: {
    label: '主页背景',
    hint: '显示在用户主页顶部的横幅。宽 960–3840 像素，宽高比 2:1 到 4:1，推荐 1500×500（3:1）；各站会按自己的比例居中裁切，重要内容放在中间。静态图用 PNG 或 JPEG（≤ 1 MB），动图用动态 WebP（≤ 3 MB，和静态图同尺寸）。',
    staticAccept: 'image/png,image/jpeg'
  }
}

export const SHOP_KIND_OPTIONS = (Object.keys(SHOP_KINDS) as ShopKind[]).map(
  (value) => ({ value, label: SHOP_KINDS[value].label })
)
