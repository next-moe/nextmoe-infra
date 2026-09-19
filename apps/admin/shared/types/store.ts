export interface StoreUsageDay {
  day: string
  total: number
  uniques: number
  bots: number
}

export interface StoreUsageApp {
  client_id: string
  name: string
  owner_user_id: number | null
  settlement_eligible: boolean
  links: number
  total: number
  uniques: number
  bots: number
  share_ppm: number
}

export interface StoreUsageLink {
  client_id: string
  app_name: string
  kind: 'purchase' | 'coupon'
  product_id: string | null
  campaign_id: number | null
  total: number
  uniques: number
  bots: number
}

export interface StoreAdminUsage {
  from: string
  to: string
  total: number
  uniques: number
  bots: number
  link_count: number
  daily: StoreUsageDay[]
  by_app: StoreUsageApp[]
  top_links: StoreUsageLink[]
  can_manage_coupons: boolean
}

export type CouponBatchStatus = 'draft' | 'published'

export interface CouponValueCount {
  face_value: number
  count: number
  allocated: number
  delivered: number
}

export interface CouponBatchSummary {
  id: number
  name: string
  period_from: string
  period_to: string
  note: string
  status: CouponBatchStatus
  created_by: number
  created_at: string
  published_at: string | null
  coupon_count: number
  points: number
  allocated: number
  delivered: number
  by_value: CouponValueCount[]
}

export interface CouponGrant {
  face_value: number
  count: number
}

export interface CouponSplitRow {
  client_id: string
  name: string
  settlement_eligible: boolean
  uniques: number
  share_ppm: number
  entitled_points: number
  grants: CouponGrant[]
  allocated_points: number
}

export interface AdminCoupon {
  id: number
  batch_id: number
  face_value: number
  code: string
  expires_on: string | null
  client_id: string | null
  delivered_at: string | null
  created_at: string
  app_name: string
}

export interface CouponBatchDetail extends CouponBatchSummary {
  coupon_list: AdminCoupon[]
  split: CouponSplitRow[]
}

export interface CouponGrantInput {
  client_id: string
  face_value: number
  count: number
}
