export type NsfwDisplay = 'hide' | 'blur' | 'show'

export interface NsfwDisplayResponse {
  nsfw_display: NsfwDisplay
  adult_confirmed_at: string | null
}

export interface PreferenceSummary {
  namespace: string
  version: number
  updated_at: string
  size_bytes: number
}

export interface PreferenceDoc {
  namespace: string
  doc: Record<string, unknown>
  version: number
  updated_at: string | null
}
