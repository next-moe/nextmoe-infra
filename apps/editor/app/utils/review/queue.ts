export interface QueueProposal {
  id: string
  site: string
  created_at: string
}

export interface QueueAnalysis {
  fields: string[]
  conflicts: Record<string, number>
}

export interface QueueFilters {
  site: string
  age: string
  field: string
  conflictOnly: boolean
}

export const AGE_OPTIONS = [
  { value: '', label: '任意时间', minDays: 0, maxDays: Infinity },
  { value: 'lt1', label: '一天内', minDays: 0, maxDays: 1 },
  { value: 'gt1', label: '超过一天', minDays: 1, maxDays: Infinity },
  { value: 'gt7', label: '超过一周', minDays: 7, maxDays: Infinity },
  { value: 'gt30', label: '超过一个月', minDays: 30, maxDays: Infinity }
] as const

const DAY_MS = 86_400_000

export const ageInDays = (iso: string, now = Date.now()) =>
  (now - new Date(iso).getTime()) / DAY_MS

export const needsAnalysis = (filters: QueueFilters) =>
  Boolean(filters.field) || filters.conflictOnly

export const filterQueue = <T extends QueueProposal>(
  items: readonly T[],
  filters: QueueFilters,
  analyses: Record<string, QueueAnalysis | undefined>,
  now = Date.now()
) => {
  const age = AGE_OPTIONS.find((option) => option.value === filters.age)
  return items.filter((item) => {
    if (filters.site && item.site !== filters.site) {
      return false
    }
    if (age && age.value) {
      const days = ageInDays(item.created_at, now)
      if (days < age.minDays || days >= age.maxDays) {
        return false
      }
    }
    if (!needsAnalysis(filters)) {
      return true
    }
    const analysis = analyses[item.id]
    if (!analysis) {
      return false
    }
    if (filters.field && !analysis.fields.includes(filters.field)) {
      return false
    }
    if (filters.conflictOnly && !Object.keys(analysis.conflicts).length) {
      return false
    }
    return true
  })
}

export const countBy = <T>(items: readonly T[], keyOf: (item: T) => string) => {
  const counts: Record<string, number> = {}
  for (const item of items) {
    const key = keyOf(item)
    counts[key] = (counts[key] ?? 0) + 1
  }
  return counts
}

export const runLimited = async <T, R>(
  items: readonly T[],
  limit: number,
  task: (item: T) => Promise<R>
) => {
  const results: R[] = new Array(items.length)
  let cursor = 0
  const workers = Array.from(
    { length: Math.min(limit, items.length) },
    async () => {
      while (cursor < items.length) {
        const index = cursor++
        results[index] = await task(items[index]!)
      }
    }
  )
  await Promise.all(workers)
  return results
}
