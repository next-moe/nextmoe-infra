export interface ExploreLocalized {
  value: string
  is_machine?: boolean
}

export interface ExploreImage {
  url: string
  width?: number | null
  height?: number | null
  sexual?: string | null
  violence?: string | null
  source?: string
}

export interface ExploreCover extends ExploreImage {
  id?: string
  cover_kind?: string
  portrait_pinned?: boolean
}

export interface ExploreRef {
  source: string
  external_id: string
}

export interface ExploreWorkBrief {
  id: string
  medium?: string
  display_name?: string
  localized?: Record<string, ExploreLocalized>
  content_rating?: string
  release_date?: string | null
  cover?: ExploreImage | null
}

export interface ExploreTitle {
  title: string
  title_kind?: string
  lang?: string
  latin?: string | null
  is_machine?: boolean
}

export interface ExploreTag {
  id?: string | null
  display_name: string
  source?: string
  tier?: string | null
  tag_kind?: string | null
  spoiler?: string
  is_sexual?: boolean
}

export interface ExploreVoice {
  id: string
  display_name: string
}

export interface ExploreWorkCharacter {
  id: string
  display_name: string
  localized?: Record<string, ExploreLocalized>
  roster_role?: string
  spoiler?: string
  image?: ExploreImage | null
  voices?: ExploreVoice[]
}

export interface ExploreCreditName {
  id: string
  display_name: string
  character_id?: string | null
}

export interface ExploreCreditGroup {
  role_key: string
  role_name: string
  credits: ExploreCreditName[]
}

export interface ExploreRating {
  source: string
  score: number
  vote_count?: number
  rank?: number | null
  distribution?: { score: number; count: number }[]
}

export interface ExplorePlaytime {
  source: string
  minutes: number
  vote_count?: number
}

export interface ExplorePopularity {
  source: string
  metric: string
  value: number
}

export interface ExploreRelease {
  id: string
  release_kind?: string
  date?: string | null
  title?: string | null
  lang?: string
  platforms?: string[]
  refs?: ExploreRef[]
}

export interface ExploreRelation {
  relation_type?: string
  phrase?: string
  work: ExploreWorkBrief
}

export interface ExploreWorkCompany {
  id: string
  display_name: string
  company_kind?: string
  attribution_role?: string
  logo?: ExploreImage | null
}

export interface ExploreSeries {
  id: string
  display_name: string
  member_count?: number
}

export interface ExploreIntro {
  lang: string
  value: string
  is_machine?: boolean
  source?: string
}

export interface ExploreWork extends ExploreWorkBrief {
  latin?: string | null
  olang?: string
  release_status?: string
  banner?: ExploreImage | null
  titles?: ExploreTitle[]
  covers?: ExploreCover[]
  screenshots?: ExploreImage[]
  tags?: ExploreTag[]
  characters?: ExploreWorkCharacter[]
  refs?: ExploreRef[]
  releases?: ExploreRelease[]
  ratings?: ExploreRating[]
  playtimes?: ExplorePlaytime[]
  popularity?: ExplorePopularity[]
  credits?: ExploreCreditGroup[]
  relations?: ExploreRelation[]
  companies?: ExploreWorkCompany[]
  series?: ExploreSeries[]
  intros?: ExploreIntro[]
  engines?: { id: string; display_name: string }[]
  links?: { source: string; url: string }[]
}

export interface ExploreAttachRow {
  work?: ExploreWorkBrief
  roster_role?: string
  spoiler?: string
  voices?: ExploreVoice[]
  roles?: {
    role_key: string
    role_name: string
    character_id?: string | null
  }[]
}

export type ExploreEntityKind =
  'characters' | 'credit-names' | 'companies' | 'works'

export interface ExploreEntityTarget {
  kind: ExploreEntityKind
  id: string
}
