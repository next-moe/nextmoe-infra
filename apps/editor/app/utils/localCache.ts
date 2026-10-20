interface CacheEnvelope<T> {
  at: number
  value: T
}

export const readLocalCache = <T>(key: string, maxAgeMs: number): T | null => {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) {
      return null
    }
    const parsed = JSON.parse(raw) as CacheEnvelope<T>
    return Date.now() - parsed.at < maxAgeMs ? parsed.value : null
  } catch {
    return null
  }
}

export const writeLocalCache = <T>(key: string, value: T) => {
  try {
    localStorage.setItem(key, JSON.stringify({ at: Date.now(), value }))
  } catch {
    return
  }
}
