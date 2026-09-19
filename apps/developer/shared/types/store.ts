export type StoreLinkKind = 'purchase' | 'coupon'

export interface StoreUsageDay {
  day: string
  total: number
  uniques: number
}

export interface StoreUsageApp {
  client_id: string
  name: string
  links: number
  total: number
  uniques: number
}

export interface StoreUsageLink {
  client_id: string
  app_name: string
  kind: StoreLinkKind
  product_id: string | null
  campaign_id: number | null
  total: number
  uniques: number
}

export interface StoreUsageSummary {
  days: number
  since: string
  until: string
  total: number
  uniques: number
  link_count: number
  daily: StoreUsageDay[]
  by_app: StoreUsageApp[]
  by_link: StoreUsageLink[]
}

export interface StoreCoupon {
  id: number
  batch_id: number
  batch_name: string
  face_value: number
  code: string
  expires_on: string | null
  delivered_at: string | null
  published_at: string | null
}

export interface StoreCouponShareApp {
  client_id: string
  name: string
  uniques: number
}

export interface StoreCouponShare {
  batch_id: number
  batch_name: string
  period_from: string
  period_to: string
  published_at: string | null
  apps: StoreCouponShareApp[]
  uniques: number
  share_ppm: number
  entitled_points: number
  allocated_points: number
}

export interface StoreCoupons {
  coupons: StoreCoupon[]
  shares: StoreCouponShare[]
}
