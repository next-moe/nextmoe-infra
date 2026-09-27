export interface ShopDecoration {
  item_id: number
  name: string
  static_url: string
  animated_url?: string
}

export type ShopSlot = 'avatar_frame' | 'profile_background'

export type ShopKind = ShopSlot | 'profile_about' | 'redeem_code'

export interface ShopCosmetics {
  avatar_frame?: ShopDecoration
  profile_background?: ShopDecoration
}

export interface ShopSite {
  id: number
  name: string
  domain: string
}

export interface ShopItem {
  id: number
  kind: ShopKind
  site_id: number | null
  status: 'draft' | 'review' | 'published' | 'retired'
  name: string
  description: string
  preview?: ShopDecoration
}

export interface ShopReward {
  item: ShopItem
  duration_days?: number
}

export interface ShopOffer {
  id: number
  site_id: number | null
  site?: ShopSite
  price: number
  rewards: ShopReward[]
  starts_at: string | null
  ends_at: string | null
  per_user_limit: number
  limit_period: '' | 'month'
  stock: number | null
  sold: number
  remaining: number | null
}

export interface ShopOwnedItem {
  item: ShopItem
  source: 'purchase' | 'grant'
  acquired_at: string
  expires_at: string | null
  active: boolean
}

export interface ShopLoadout {
  slot: ShopSlot
  site_id: number
  item_id: number
}

export interface ShopCode {
  item_id: number
  code: string
  expires_on: string | null
}

export interface ShopOrder {
  id: number
  price: number
  status: 'completed' | 'refunded'
  rewards: ShopReward[]
  codes?: ShopCode[]
  created_at: string
}

export interface ShopInventory {
  balance: number
  items: ShopOwnedItem[]
  loadout: ShopLoadout[]
  orders: ShopOrder[]
  limit_used: Record<string, number>
}

export interface ShopPurchased {
  order: ShopOrder
  balance: number
  replay: boolean
}
