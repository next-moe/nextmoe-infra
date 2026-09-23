export type ShopItemStatus = 'draft' | 'review' | 'published' | 'retired'
export type ShopOfferStatus = 'draft' | 'active' | 'retired'

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
  kind: string
  site_id: number | null
  status: ShopItemStatus
  name: string
  description: string
  render: { static: string; animated?: string }
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
  status: ShopOfferStatus
  price: number
  rewards: { item: ShopItem; duration_days?: number }[]
  starts_at: string | null
  ends_at: string | null
  per_user_limit: number
  stock: number | null
  sold: number
  sort_order: number
  created_at: string
}

export interface ShopOrder {
  id: number
  offer_id: number
  price: number
  status: 'completed' | 'refunded'
  rewards: { item: ShopItem; duration_days?: number }[]
  created_at: string
  refunded_at: string | null
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
