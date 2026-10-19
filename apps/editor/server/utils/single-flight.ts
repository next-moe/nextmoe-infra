export const createSingleFlight = <T>(lingerMs: number) => {
  const pending = new Map<string, Promise<T>>()
  return (key: string, run: () => Promise<T>) => {
    const existing = pending.get(key)
    if (existing) {
      return existing
    }
    const promise = run().finally(() => {
      setTimeout(() => pending.delete(key), lingerMs)
    })
    pending.set(key, promise)
    return promise
  }
}
