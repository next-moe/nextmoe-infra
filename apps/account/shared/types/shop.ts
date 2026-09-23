export interface ShopDecoration {
  item_id: number
  name: string
  static_url: string
  animated_url?: string
}

export interface ShopCosmetics {
  avatar_frame?: ShopDecoration
}

export interface ShopItem {
  id: number
  kind: string
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
  price: number
  rewards: ShopReward[]
  starts_at: string | null
  ends_at: string | null
  per_user_limit: number
  stock: number | null
  sold: number
}

export interface ShopOwnedItem {
  item: ShopItem
  source: 'purchase' | 'grant'
  acquired_at: string
  expires_at: string | null
  active: boolean
}

export interface ShopLoadout {
  slot: string
  site_id: number
  item_id: number
}

export interface ShopOrder {
  id: number
  price: number
  status: 'completed' | 'refunded'
  rewards: ShopReward[]
  created_at: string
}

export interface ShopInventory {
  balance: number
  items: ShopOwnedItem[]
  loadout: ShopLoadout[]
  orders: ShopOrder[]
}

export interface ShopPurchased {
  order: ShopOrder
  balance: number
  replay: boolean
}
