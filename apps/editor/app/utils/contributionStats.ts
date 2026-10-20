export interface ActivityWeek {
  start: string
  count: number
}

const DAY_MS = 86_400_000

const startOfWeek = (time: number) => {
  const date = new Date(time)
  date.setHours(0, 0, 0, 0)
  const weekday = (date.getDay() + 6) % 7
  return date.getTime() - weekday * DAY_MS
}

export const weeklyActivity = (
  timestamps: (string | null | undefined)[],
  weeks: number,
  now = Date.now()
): ActivityWeek[] => {
  const current = startOfWeek(now)
  const buckets = Array.from({ length: weeks }, (_, index) => ({
    start: current - (weeks - 1 - index) * 7 * DAY_MS,
    count: 0
  }))
  const first = buckets[0]?.start ?? current
  for (const iso of timestamps) {
    const time = iso ? Date.parse(iso) : Number.NaN
    if (Number.isNaN(time) || time < first) {
      continue
    }
    const index = Math.floor((startOfWeek(time) - first) / (7 * DAY_MS))
    const bucket = buckets[index]
    if (bucket) {
      bucket.count++
    }
  }
  return buckets.map((bucket) => ({
    start: new Date(bucket.start).toISOString(),
    count: bucket.count
  }))
}
