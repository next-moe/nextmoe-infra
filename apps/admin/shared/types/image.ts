export type ImageReviewStatus =
  | 'pending'
  | 'approved'
  | 'rejected'
  | 'manual_review'
  | 'unknown'

export interface ImageAdminRow {
  hash: string
  url: string
  variant_urls: Record<string, string>
  width: number
  height: number
  size_bytes: number
  mime: string
  review_status: ImageReviewStatus
  review_labels?: unknown
  first_uploader_sub?: string
  first_uploader_client?: string
  created_at: string
  last_referenced_at: string
  deleted_at?: string | null
}

export interface ImageAdminListResponse {
  items: ImageAdminRow[]
  total: number
  page: number
  limit: number
}

export interface ImageAdminStats {
  upload_count: number
  unique_images: number
  deduplicated_count: number
  total_bytes: number
  by_site?: Record<string, { count: number; unique: number }>
  review_pending: number
  review_rejected: number
}
