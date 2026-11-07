export const isSafeInternalPath = (r: unknown): r is string => {
  if (typeof r !== 'string' || !r.startsWith('/')) return false
  try {
    return new URL(r, 'https://guard.internal').origin === 'https://guard.internal'
  } catch {
    return false
  }
}
