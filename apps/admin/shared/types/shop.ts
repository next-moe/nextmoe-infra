export type ShopItemStatus = 'draft' | 'review' | 'published' | 'retired'
export type ShopOfferStatus = 'draft' | 'active' | 'retired'
export type ShopKind =
  'avatar_frame' | 'profile_background' | 'profile_about' | 'redeem_code'
export type ShopLimitPeriod = '' | 'month'

export interface ShopSite {
  id: number
  name: string
  domain: string
}

export interface ShopDecoration {
  item_id: number
  name: string
  static_url: string
  animated_url?: string
}

export interface ShopAsset {
  hash: string
  key: string
  content_type: string
  width: number
  height: number
  animated: boolean
  bytes: number
  url: string
  created_at: string
}

export interface ShopItem {
  id: number
  kind: ShopKind
  site_id: number | null
  status: ShopItemStatus
  name: string
  description: string
  render: { static?: string; animated?: string }
  preview?: ShopDecoration
  published_at: string | null
  created_at: string
}

export interface ShopRewardInput {
  item_id: number
  duration_days?: number
}

export interface ShopOffer {
  id: number
  site_id: number | null
  site?: ShopSite
  status: ShopOfferStatus
  price: number
  rewards: { item: ShopItem; duration_days?: number }[]
  starts_at: string | null
  ends_at: string | null
  per_user_limit: number
  limit_period: ShopLimitPeriod
  stock: number | null
  sold: number
  remaining: number | null
  sort_order: number
  created_at: string
}

export interface ShopOrder {
  id: number
  offer_id: number
  price: number
  status: 'completed' | 'refunded'
  rewards: { item: ShopItem; duration_days?: number }[]
  codes?: { item_id: number; code: string; expires_on: string | null }[]
  created_at: string
  refunded_at: string | null
}

export interface ShopCode {
  id: number
  item_id: number
  code: string
  expires_on: string | null
  order_id: number | null
  user_id: number | null
  sold_at: string | null
  created_at: string
}

export interface ShopCodePool {
  total: number
  sellable: number
  sold: number
  shelf_days: number
  codes: ShopCode[]
}

export interface ShopEntitlement {
  id: number
  item_id: number
  source: 'purchase' | 'grant'
  note: string
  acquired_at: string
  expires_at: string | null
  revoked_at: string | null
  active: boolean
  item: ShopItem
}

export interface ShopUserView {
  balance: number
  entitlements: ShopEntitlement[]
  loadout: { slot: string; site_id: number; item_id: number }[]
  orders: ShopOrder[]
}
