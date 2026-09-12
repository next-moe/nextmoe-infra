export interface MoemoepointLogItem {
  id: number
  delta: number
  reason: string
  source_app: string
  source_name?: string
  ref: string
  created_at: string
  actor_user_id?: number
  note?: string
}
