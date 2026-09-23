import type { KunUIColor } from '@kungal/ui-core'
import type { ShopItemStatus, ShopOfferStatus } from '~~/shared/types/shop'

export const SHOP_ITEM_STATUS: Record<ShopItemStatus, { label: string; color: KunUIColor }> = {
  draft: { label: '草稿', color: 'default' },
  review: { label: '待审核', color: 'warning' },
  published: { label: '已发布', color: 'success' },
  retired: { label: '已下架', color: 'danger' },
}

export const SHOP_ITEM_ACTIONS: Record<ShopItemStatus, { action: string; label: string }[]> = {
  draft: [
    { action: 'submit', label: '提交审核' },
    { action: 'publish', label: '发布' },
  ],
  review: [
    { action: 'publish', label: '发布' },
    { action: 'reject', label: '驳回' },
  ],
  published: [{ action: 'retire', label: '下架' }],
  retired: [{ action: 'relist', label: '重新发布' }],
}

export const SHOP_OFFER_STATUS: Record<ShopOfferStatus, { label: string; color: KunUIColor }> = {
  draft: { label: '未上架', color: 'default' },
  active: { label: '在售', color: 'success' },
  retired: { label: '已下架', color: 'danger' },
}
