
export interface DailyRegistration {
  date: string // YYYY-MM-DD
  count: number
}

export interface RegistrationSummary {
  total: number
  daily_average: number
  peak_date: string
  peak_count: number
  today: number
  total_all_time: number
  prev_period_total: number
  growth_percent: number
}

export interface RegistrationStats {
  range_days: number
  timezone: string
  series: DailyRegistration[]
  summary: RegistrationSummary
}

export interface HourlyRegistration {
  hour: number // 0-23
  count: number
}

export interface HourlyStats {
  date: string
  timezone: string
  series: HourlyRegistration[]
  total: number
}

export interface ActiveSummary {
  today: number
  daily_average: number
  peak_date: string
  peak_count: number
  unique: number
  prev_period_unique: number
  growth_percent: number
  tracked_since?: string
}

export interface ActiveStats {
  range_days: number
  timezone: string
  series: DailyRegistration[]
  summary: ActiveSummary
}
